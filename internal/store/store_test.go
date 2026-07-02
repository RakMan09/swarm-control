package store

import (
	"testing"
	"time"

	"github.com/rakman09/swarm-control/internal/config"
	"github.com/rakman09/swarm-control/internal/metrics"
	"github.com/rakman09/swarm-control/internal/telemetry"
)

// newTestWriter builds a Writer without a DB connection so the bounded-queue
// backpressure behaviour can be tested in isolation.
func newTestWriter(queue int, dropOnFull bool) *Writer {
	reg := metrics.NewRegistry()
	cfg := config.Ingestion{QueueSize: queue, BatchSize: 100, BatchTimeout: time.Second, DropOnFull: dropOnFull}
	return &Writer{
		cfg:       cfg,
		ch:        make(chan telemetry.Message, queue),
		reg:       reg,
		mAccepted: reg.Counter("ingest_messages_total"),
		mDropped:  reg.Counter("ingest_dropped_total"),
		mQueue:    reg.Gauge("ingest_queue_depth"),
	}
}

func sample() telemetry.Message {
	return telemetry.Message{VehicleID: "v", TS: time.Now(), BatteryPct: 50}
}

func TestEnqueueDropsWhenFull(t *testing.T) {
	w := newTestWriter(2, true)
	if !w.Enqueue(sample()) || !w.Enqueue(sample()) {
		t.Fatal("first two enqueues should be accepted")
	}
	if w.Enqueue(sample()) {
		t.Fatal("third enqueue should be dropped when queue is full and DropOnFull=true")
	}
	if w.mDropped.Value() != 1 {
		t.Fatalf("expected 1 drop, got %d", w.mDropped.Value())
	}
	if w.mAccepted.Value() != 2 {
		t.Fatalf("expected 2 accepted, got %d", w.mAccepted.Value())
	}
	if w.QueueLen() != 2 {
		t.Fatalf("expected queue depth 2, got %d", w.QueueLen())
	}
}

func TestEnqueueBlockingModeAcceptsWhenDrained(t *testing.T) {
	w := newTestWriter(1, false) // block-on-full
	if !w.Enqueue(sample()) {
		t.Fatal("first enqueue should be accepted")
	}
	// Drain concurrently so a blocking enqueue can proceed.
	done := make(chan bool, 1)
	go func() { done <- w.Enqueue(sample()) }()
	select {
	case m := <-w.ch:
		_ = m
	case <-time.After(time.Second):
		t.Fatal("expected to drain a message")
	}
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("blocking enqueue should eventually succeed")
		}
	case <-time.After(time.Second):
		t.Fatal("blocking enqueue did not complete after drain")
	}
}
