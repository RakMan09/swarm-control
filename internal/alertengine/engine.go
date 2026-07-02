// Package alertengine evaluates streaming rules over live telemetry: geofence
// breach, low battery, lost-comms (absence of data), and statistical anomalies.
package alertengine

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rakman09/swarm-control/internal/bus"
	"github.com/rakman09/swarm-control/internal/telemetry"
)

// FireFunc receives an alert when a rule triggers.
type FireFunc func(bus.Alert)

// Config are the tunable thresholds for the engine (subset of config.Alerts).
type Config struct {
	LowBatteryPct  float64
	LostCommsAfter time.Duration
	AnomalyK       float64
	AnomalyWindow  int
	Fence          Polygon
}

// Engine holds rule state and fires alerts through a callback. It is safe for
// concurrent use by the telemetry consumer and the lost-comms sweeper.
type Engine struct {
	cfg  Config
	anom *AnomalyDetector
	fire FireFunc

	mu       sync.Mutex
	lastSeen map[string]*vehicleState
	active   map[string]bool // "vehicleId|type" -> currently alerting

	seq atomic.Int64
}

type vehicleState struct {
	last      time.Time
	lat, lon  float64
	lostFired bool
}

// New builds an engine.
func New(cfg Config, fire FireFunc) *Engine {
	if cfg.AnomalyWindow < 2 {
		cfg.AnomalyWindow = 30
	}
	return &Engine{
		cfg:      cfg,
		anom:     NewAnomalyDetector(cfg.AnomalyWindow, cfg.AnomalyK),
		fire:     fire,
		lastSeen: map[string]*vehicleState{},
		active:   map[string]bool{},
	}
}

// ProcessTelemetry runs the per-message rules for a validated message.
func (e *Engine) ProcessTelemetry(m telemetry.Message) {
	now := time.Now()
	e.mu.Lock()
	st, ok := e.lastSeen[m.VehicleID]
	if !ok {
		st = &vehicleState{}
		e.lastSeen[m.VehicleID] = st
	}
	st.last = now
	st.lat, st.lon = m.Lat, m.Lon
	// Vehicle reported again; re-arm lost-comms so it can fire on a future gap.
	st.lostFired = false
	e.mu.Unlock()

	// Geofence breach.
	inside := e.cfg.Fence.Contains(Point{Lat: m.Lat, Lon: m.Lon})
	e.transition(m.VehicleID, "geofence", !inside, func() bus.Alert {
		return e.newAlert(m, "geofence", "critical",
			fmt.Sprintf("%s left the geofence", m.VehicleID), 0)
	})

	// Low battery.
	e.transition(m.VehicleID, "low_battery", m.BatteryPct <= e.cfg.LowBatteryPct, func() bus.Alert {
		return e.newAlert(m, "low_battery", "warning",
			fmt.Sprintf("%s battery low: %.1f%%", m.VehicleID, m.BatteryPct), m.BatteryPct)
	})

	// Statistical temperature anomaly (edge-triggered per sample).
	if anom, mean, std := e.anom.Observe(m.VehicleID, m.Temp); anom {
		a := e.newAlert(m, "anomaly", "warning",
			fmt.Sprintf("%s temp anomaly: %.1f (mean %.1f, std %.1f)", m.VehicleID, m.Temp, mean, std), m.Temp)
		e.fire(a)
	}
}

// SweepLostComms fires a lost-comms alert for any vehicle that has not reported
// within LostCommsAfter. This detects the *absence* of data, which a stream
// trigger cannot, so it runs on a timer.
func (e *Engine) SweepLostComms(now time.Time) {
	var toFire []bus.Alert
	e.mu.Lock()
	for id, st := range e.lastSeen {
		if st.lostFired {
			continue
		}
		if now.Sub(st.last) > e.cfg.LostCommsAfter {
			st.lostFired = true
			toFire = append(toFire, bus.Alert{
				ID:         e.id(),
				VehicleID:  id,
				Type:       "lost_comms",
				Severity:   "critical",
				Message:    fmt.Sprintf("%s lost comms (no telemetry for %s)", id, now.Sub(st.last).Round(time.Second)),
				Lat:        st.lat,
				Lon:        st.lon,
				TS:         st.last.UnixMilli(),
				DetectedAt: now.UnixMilli(),
			})
		}
	}
	e.mu.Unlock()
	for _, a := range toFire {
		e.fire(a)
	}
}

// transition fires when a condition flips from false->true and clears the
// active flag when it flips back, so alerts don't spam every message.
func (e *Engine) transition(vehicleID, typ string, cond bool, build func() bus.Alert) {
	key := vehicleID + "|" + typ
	e.mu.Lock()
	was := e.active[key]
	e.active[key] = cond
	e.mu.Unlock()
	if cond && !was {
		e.fire(build())
	}
}

func (e *Engine) newAlert(m telemetry.Message, typ, sev, msg string, val float64) bus.Alert {
	return bus.Alert{
		ID:         e.id(),
		VehicleID:  m.VehicleID,
		Type:       typ,
		Severity:   sev,
		Message:    msg,
		Value:      val,
		Lat:        m.Lat,
		Lon:        m.Lon,
		TS:         m.TS.UnixMilli(),
		DetectedAt: time.Now().UnixMilli(),
	}
}

func (e *Engine) id() string {
	return fmt.Sprintf("a-%d-%d", time.Now().UnixNano(), e.seq.Add(1))
}
