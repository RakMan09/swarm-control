// Package store implements the TimescaleDB telemetry writer. It applies a
// bounded queue + batched COPY inserts so that a burst of thousands of devices
// degrades gracefully (shed or delay) instead of overwhelming the database.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rakman09/swarm-control/internal/config"
	"github.com/rakman09/swarm-control/internal/metrics"
	"github.com/rakman09/swarm-control/internal/telemetry"
)

// Writer batches telemetry into TimescaleDB behind a bounded channel.
type Writer struct {
	pool *pgxpool.Pool
	cfg  config.Ingestion
	ch   chan telemetry.Message

	reg        *metrics.Registry
	mAccepted  *metrics.Counter
	mDropped   *metrics.Counter
	mRows      *metrics.Counter
	mBatches   *metrics.Counter
	mQueue     *metrics.Gauge
	mInsertLat *metrics.Histogram
}

// New connects to Postgres/Timescale and returns a Writer. Call Run to start
// the background flusher.
func New(ctx context.Context, cfg config.Ingestion, reg *metrics.Registry) (*Writer, error) {
	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Writer{
		pool:       pool,
		cfg:        cfg,
		ch:         make(chan telemetry.Message, cfg.QueueSize),
		reg:        reg,
		mAccepted:  reg.Counter("ingest_messages_total"),
		mDropped:   reg.Counter("ingest_dropped_total"),
		mRows:      reg.Counter("ingest_rows_written_total"),
		mBatches:   reg.Counter("ingest_batches_total"),
		mQueue:     reg.Gauge("ingest_queue_depth"),
		mInsertLat: reg.Histogram("ingest_insert_latency_ms", 4096),
	}, nil
}

// Enqueue submits a message. When the queue is full it either drops (shedding
// load) or blocks (delaying the producer), per config.DropOnFull. Returns true
// if the message was accepted.
func (w *Writer) Enqueue(m telemetry.Message) bool {
	if w.cfg.DropOnFull {
		select {
		case w.ch <- m:
			w.mAccepted.Inc()
			w.mQueue.Set(int64(len(w.ch)))
			return true
		default:
			w.mDropped.Inc()
			return false
		}
	}
	w.ch <- m
	w.mAccepted.Inc()
	w.mQueue.Set(int64(len(w.ch)))
	return true
}

// QueueLen reports the current queue depth (used for tests/metrics).
func (w *Writer) QueueLen() int { return len(w.ch) }

// Run consumes the queue, accumulating batches that flush on size or timeout.
// It returns when ctx is cancelled, flushing any remaining rows.
func (w *Writer) Run(ctx context.Context) {
	batch := make([]telemetry.Message, 0, w.cfg.BatchSize)
	ticker := time.NewTicker(w.cfg.BatchTimeout)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		start := time.Now()
		if err := w.insert(ctx, batch); err != nil {
			// Insert failures should not wedge the pipeline; count and move on.
			w.reg.Counter("ingest_insert_errors_total").Inc()
		} else {
			w.mBatches.Inc()
			w.mRows.Add(int64(len(batch)))
			w.mInsertLat.ObserveDuration(time.Since(start))
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case m := <-w.ch:
			batch = append(batch, m)
			w.mQueue.Set(int64(len(w.ch)))
			if len(batch) >= w.cfg.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// insert performs a single batched COPY of the accumulated rows.
func (w *Writer) insert(ctx context.Context, batch []telemetry.Message) error {
	rows := make([][]any, len(batch))
	for i, m := range batch {
		status := m.Status
		if status == "" {
			status = "ok"
		}
		rows[i] = []any{m.VehicleID, m.TS, m.Lat, m.Lon, m.HeadingDeg, m.Speed, m.BatteryPct, m.Temp, status}
	}
	_, err := w.pool.CopyFrom(ctx,
		pgx.Identifier{"telemetry"},
		[]string{"vehicle_id", "ts", "lat", "lon", "heading", "speed", "battery_pct", "temp", "status"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// Pool exposes the underlying connection pool for read queries (history API).
func (w *Writer) Pool() *pgxpool.Pool { return w.pool }

// Close releases the connection pool.
func (w *Writer) Close() { w.pool.Close() }
