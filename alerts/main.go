// Command alerts consumes the validated telemetry stream and evaluates the
// streaming rules (geofence, low battery, lost-comms, anomaly), publishing
// fired alerts back onto the bus and into a capped recent-alerts list.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rakman09/swarm-control/internal/alertengine"
	"github.com/rakman09/swarm-control/internal/bus"
	"github.com/rakman09/swarm-control/internal/config"
	"github.com/rakman09/swarm-control/internal/metrics"
	"github.com/rakman09/swarm-control/internal/twin"
)

const recentAlertsKey = "alerts:recent"
const recentAlertsMax = 500

func main() {
	cfg := config.LoadAlerts()
	ctx, cancel := signalContext()
	defer cancel()

	reg := metrics.NewRegistry()
	reg.Serve(cfg.MetricsAddr)
	log.Printf("alerts: metrics on %s", cfg.MetricsAddr)
	fired := reg.Counter("alerts_fired_total")
	latency := reg.Histogram("alert_event_to_alert_ms", 4096)

	fence, err := alertengine.ParsePolygon(cfg.GeofencePolygon)
	if err != nil {
		log.Fatalf("alerts: bad geofence polygon: %v", err)
	}

	twins := twin.New(cfg.RedisAddr)
	if err := twins.Ping(ctx); err != nil {
		log.Fatalf("alerts: redis: %v", err)
	}
	rdb := twins.Client()
	b := bus.NewWithClient(rdb)

	fire := func(a bus.Alert) {
		fired.Inc()
		if a.TS > 0 {
			latency.Observe(float64(a.DetectedAt - a.TS))
		}
		pctx, c := context.WithTimeout(ctx, 2*time.Second)
		defer c()
		_ = b.PublishAlert(pctx, a)
		if raw, err := json.Marshal(a); err == nil {
			pipe := rdb.Pipeline()
			pipe.LPush(pctx, recentAlertsKey, raw)
			pipe.LTrim(pctx, recentAlertsKey, 0, recentAlertsMax-1)
			_, _ = pipe.Exec(pctx)
		}
		log.Printf("alert: %s %s %s", a.Severity, a.Type, a.Message)
	}

	engine := alertengine.New(alertengine.Config{
		LowBatteryPct:  cfg.LowBatteryPct,
		LostCommsAfter: cfg.LostCommsAfter,
		AnomalyK:       cfg.AnomalyK,
		AnomalyWindow:  cfg.AnomalyWindow,
		Fence:          fence,
	}, fire)

	// Lost-comms sweep: detect the absence of telemetry on a timer.
	go func() {
		t := time.NewTicker(cfg.SweepInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				engine.SweepLostComms(now)
			}
		}
	}()

	stream := b.SubscribeTelemetry(ctx)
	log.Printf("alerts: running (lowBatt=%.0f%% lostComms=%s anomalyK=%.1f)", cfg.LowBatteryPct, cfg.LostCommsAfter, cfg.AnomalyK)
	for {
		select {
		case <-ctx.Done():
			log.Printf("alerts: shutting down")
			return
		case m, ok := <-stream:
			if !ok {
				return
			}
			engine.ProcessTelemetry(m)
		}
	}
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
