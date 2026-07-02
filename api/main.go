// Command api serves the dashboard: fleet snapshot and history reads, recent
// alerts, a command proxy, and a websocket that streams coalesced fleet state
// and live alerts.
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rakman09/swarm-control/internal/bus"
	"github.com/rakman09/swarm-control/internal/config"
	"github.com/rakman09/swarm-control/internal/twin"
)

const recentAlertsKey = "alerts:recent"

var allowedWindows = map[string]bool{
	"5m": true, "15m": true, "1h": true, "6h": true, "24h": true, "168h": true,
}

type server struct {
	cfg    config.API
	twins  *twin.Store
	bus    *bus.Bus
	pool   *pgxpool.Pool // may be nil if Postgres is unavailable
	hub    *hub
	client *http.Client

	mu     sync.RWMutex
	latest map[string]twin.State // live fleet state coalesced from the bus
}

func main() {
	cfg := config.LoadAPI()
	ctx, cancel := signalContext()
	defer cancel()

	twins := twin.New(cfg.RedisAddr)
	if err := twins.Ping(ctx); err != nil {
		log.Fatalf("api: redis: %v", err)
	}
	b := bus.NewWithClient(twins.Client())

	var pool *pgxpool.Pool
	if p, err := pgxpool.New(ctx, cfg.PostgresDSN); err == nil && p.Ping(ctx) == nil {
		pool = p
		defer pool.Close()
	} else {
		log.Printf("api: Postgres unavailable, history endpoint disabled")
	}

	s := &server{
		cfg:    cfg,
		twins:  twins,
		bus:    b,
		pool:   pool,
		hub:    newHub(),
		client: &http.Client{Timeout: 5 * time.Second},
		latest: map[string]twin.State{},
	}

	// Seed live state from the twin snapshot, then keep it fresh from the bus.
	if snap, err := twins.Snapshot(ctx); err == nil {
		for _, st := range snap {
			s.latest[st.VehicleID] = st
		}
	}
	go s.consumeTelemetry(ctx)
	go s.consumeAlerts(ctx)
	go s.broadcastFleet(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/api/fleet", s.handleFleet)
	mux.HandleFunc("/api/alerts", s.handleAlerts)
	mux.HandleFunc("/api/commands", s.handleCommandProxy)
	mux.HandleFunc("/api/vehicles/", s.handleVehicle) // /api/vehicles/{id}/history
	mux.HandleFunc("/ws", s.hub.handle)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}
	go func() {
		log.Printf("api: HTTP on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("api: http: %v", err)
		}
	}()

	<-ctx.Done()
	sc, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	_ = srv.Shutdown(sc)
	log.Printf("api: shutting down")
}

func (s *server) consumeTelemetry(ctx context.Context) {
	stream := s.bus.SubscribeTelemetry(ctx)
	for m := range stream {
		s.mu.Lock()
		s.latest[m.VehicleID] = twin.State{
			VehicleID: m.VehicleID, TS: m.TS.UnixMilli(), Lat: m.Lat, Lon: m.Lon,
			HeadingDeg: m.HeadingDeg, Speed: m.Speed, BatteryPct: m.BatteryPct,
			Temp: m.Temp, Status: m.Status, LastSeen: time.Now().UnixMilli(),
		}
		s.mu.Unlock()
	}
}

func (s *server) consumeAlerts(ctx context.Context) {
	for a := range s.bus.SubscribeAlerts(ctx) {
		s.hub.broadcast("alert", a)
	}
}

// broadcastFleet pushes a coalesced fleet snapshot at a fixed cadence so the
// dashboard update rate stays bounded no matter how fast telemetry arrives.
func (s *server) broadcastFleet(ctx context.Context) {
	t := time.NewTicker(750 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.hub.clientCount() == 0 {
				continue
			}
			s.mu.RLock()
			out := make([]twin.State, 0, len(s.latest))
			for _, st := range s.latest {
				out = append(out, st)
			}
			s.mu.RUnlock()
			s.hub.broadcast("fleet", out)
		}
	}
}

func (s *server) handleFleet(w http.ResponseWriter, r *http.Request) {
	cors(w)
	snap, err := s.twins.Snapshot(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	cors(w)
	raws, err := s.twins.Client().LRange(r.Context(), recentAlertsKey, 0, 199).Result()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]json.RawMessage, 0, len(raws))
	for _, r := range raws {
		out = append(out, json.RawMessage(r))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleVehicle routes /api/vehicles/{id}/history.
func (s *server) handleVehicle(w http.ResponseWriter, r *http.Request) {
	cors(w)
	rest := strings.TrimPrefix(r.URL.Path, "/api/vehicles/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[1] != "history" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	id := parts[0]
	if s.pool == nil {
		http.Error(w, "history unavailable", http.StatusServiceUnavailable)
		return
	}
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "24h"
	}
	if !allowedWindows[window] {
		http.Error(w, "invalid window", http.StatusBadRequest)
		return
	}
	resolution := r.URL.Query().Get("resolution") // "raw" | "1m" (default)

	points, took, err := s.queryHistory(r.Context(), id, window, resolution)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"vehicleId":  id,
		"window":     window,
		"resolution": resolutionLabel(resolution),
		"queryMs":    took.Milliseconds(),
		"points":     points,
	})
}

type historyPoint struct {
	TS         int64   `json:"ts"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	Speed      float64 `json:"speed"`
	BatteryPct float64 `json:"batteryPct"`
	Temp       float64 `json:"temp"`
}

func (s *server) queryHistory(ctx context.Context, id, window, resolution string) ([]historyPoint, time.Duration, error) {
	var sql string
	if resolution == "raw" {
		sql = `SELECT ts, lat, lon, speed, battery_pct, temp
		       FROM telemetry
		       WHERE vehicle_id=$1 AND ts > now() - $2::interval
		       ORDER BY ts`
	} else {
		sql = `SELECT bucket, avg_lat, avg_lon, avg_speed, avg_battery, avg_temp
		       FROM telemetry_1m
		       WHERE vehicle_id=$1 AND bucket > now() - $2::interval
		       ORDER BY bucket`
	}
	start := time.Now()
	rows, err := s.pool.Query(ctx, sql, id, window)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var pts []historyPoint
	for rows.Next() {
		var p historyPoint
		var ts time.Time
		if err := rows.Scan(&ts, &p.Lat, &p.Lon, &p.Speed, &p.BatteryPct, &p.Temp); err != nil {
			return nil, 0, err
		}
		p.TS = ts.UnixMilli()
		pts = append(pts, p)
	}
	return pts, time.Since(start), rows.Err()
}

// handleCommandProxy forwards POST /api/commands to the command service.
func (s *server) handleCommandProxy(w http.ResponseWriter, r *http.Request) {
	cors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	target := strings.TrimRight(s.cfg.CommandBaseURL, "/") + "/commands"
	var req *http.Request
	var err error
	if r.Method == http.MethodPost {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		req, err = http.NewRequestWithContext(r.Context(), http.MethodPost, target, strings.NewReader(string(body)))
		if req != nil {
			req.Header.Set("Content-Type", "application/json")
		}
	} else {
		q := r.URL.RawQuery
		if q != "" {
			target += "?" + q
		}
		req, err = http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := s.client.Do(req)
	if err != nil {
		http.Error(w, "command service unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func resolutionLabel(r string) string {
	if r == "raw" {
		return "raw"
	}
	return "1m-continuous-aggregate"
}

func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}
