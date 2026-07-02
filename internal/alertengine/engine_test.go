package alertengine

import (
	"sync"
	"testing"
	"time"

	"github.com/rakman09/swarm-control/internal/bus"
	"github.com/rakman09/swarm-control/internal/telemetry"
)

type collector struct {
	mu     sync.Mutex
	alerts []bus.Alert
}

func (c *collector) fire(a bus.Alert) {
	c.mu.Lock()
	c.alerts = append(c.alerts, a)
	c.mu.Unlock()
}

func (c *collector) countType(t string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, a := range c.alerts {
		if a.Type == t {
			n++
		}
	}
	return n
}

func newEngine(c *collector) *Engine {
	return New(Config{
		LowBatteryPct:  15,
		LostCommsAfter: 100 * time.Millisecond,
		AnomalyK:       3,
		AnomalyWindow:  10,
		Fence:          DefaultPolygon(),
	}, c.fire)
}

func msg(id string, lat, lon, batt, temp float64) telemetry.Message {
	return telemetry.Message{VehicleID: id, TS: time.Now(), Lat: lat, Lon: lon, HeadingDeg: 0, Speed: 1, BatteryPct: batt, Temp: temp, Status: "active"}
}

func TestGeofenceBreachFiresOnce(t *testing.T) {
	c := &collector{}
	e := newEngine(c)
	// Inside: no alert.
	e.ProcessTelemetry(msg("v", 37.5, -122.05, 90, 25))
	// Outside: fire once.
	e.ProcessTelemetry(msg("v", 37.5, -121.0, 90, 25))
	e.ProcessTelemetry(msg("v", 37.5, -121.0, 90, 25))
	if got := c.countType("geofence"); got != 1 {
		t.Fatalf("expected exactly 1 geofence alert, got %d", got)
	}
}

func TestLowBatteryFires(t *testing.T) {
	c := &collector{}
	e := newEngine(c)
	e.ProcessTelemetry(msg("v", 37.5, -122.05, 50, 25))
	e.ProcessTelemetry(msg("v", 37.5, -122.05, 10, 25))
	if got := c.countType("low_battery"); got != 1 {
		t.Fatalf("expected 1 low_battery alert, got %d", got)
	}
}

func TestLostCommsSweepFires(t *testing.T) {
	c := &collector{}
	e := newEngine(c)
	e.ProcessTelemetry(msg("v", 37.5, -122.05, 90, 25))
	// No sweep yet: recently seen.
	e.SweepLostComms(time.Now())
	if got := c.countType("lost_comms"); got != 0 {
		t.Fatalf("expected 0 lost_comms before timeout, got %d", got)
	}
	// After the timeout window elapses.
	e.SweepLostComms(time.Now().Add(200 * time.Millisecond))
	if got := c.countType("lost_comms"); got != 1 {
		t.Fatalf("expected 1 lost_comms after timeout, got %d", got)
	}
	// Should not double-fire on the next sweep.
	e.SweepLostComms(time.Now().Add(400 * time.Millisecond))
	if got := c.countType("lost_comms"); got != 1 {
		t.Fatalf("lost_comms should fire once, got %d", got)
	}
}

func TestLostCommsRearmsAfterReturn(t *testing.T) {
	c := &collector{}
	e := newEngine(c)
	e.ProcessTelemetry(msg("v", 37.5, -122.05, 90, 25))
	e.SweepLostComms(time.Now().Add(200 * time.Millisecond))
	// Vehicle reports again, then goes silent again.
	e.ProcessTelemetry(msg("v", 37.5, -122.05, 90, 25))
	e.SweepLostComms(time.Now().Add(400 * time.Millisecond))
	if got := c.countType("lost_comms"); got != 2 {
		t.Fatalf("expected lost_comms to re-arm and fire twice, got %d", got)
	}
}
