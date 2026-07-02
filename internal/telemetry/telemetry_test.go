package telemetry

import (
	"testing"
	"time"
)

func validRaw() []byte {
	return []byte(`{"vehicleId":"veh-1","ts":"2026-01-01T00:00:00Z","lat":37.5,"lon":-122.0,"headingDeg":90,"speed":5,"batteryPct":80,"temp":25,"status":"active"}`)
}

func TestParseValid(t *testing.T) {
	m, err := Parse(validRaw())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.VehicleID != "veh-1" || m.BatteryPct != 80 {
		t.Fatalf("unexpected message: %+v", m)
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	cases := map[string][]byte{
		"bad json":       []byte(`{not json`),
		"missing id":     []byte(`{"ts":"2026-01-01T00:00:00Z","lat":1,"lon":1,"headingDeg":0,"speed":0,"batteryPct":50,"temp":10}`),
		"missing ts":     []byte(`{"vehicleId":"v","lat":1,"lon":1,"headingDeg":0,"speed":0,"batteryPct":50,"temp":10}`),
		"lat range":      []byte(`{"vehicleId":"v","ts":"2026-01-01T00:00:00Z","lat":123,"lon":1,"headingDeg":0,"speed":0,"batteryPct":50,"temp":10}`),
		"lon range":      []byte(`{"vehicleId":"v","ts":"2026-01-01T00:00:00Z","lat":1,"lon":999,"headingDeg":0,"speed":0,"batteryPct":50,"temp":10}`),
		"heading range":  []byte(`{"vehicleId":"v","ts":"2026-01-01T00:00:00Z","lat":1,"lon":1,"headingDeg":400,"speed":0,"batteryPct":50,"temp":10}`),
		"negative speed": []byte(`{"vehicleId":"v","ts":"2026-01-01T00:00:00Z","lat":1,"lon":1,"headingDeg":0,"speed":-1,"batteryPct":50,"temp":10}`),
		"battery range":  []byte(`{"vehicleId":"v","ts":"2026-01-01T00:00:00Z","lat":1,"lon":1,"headingDeg":0,"speed":0,"batteryPct":150,"temp":10}`),
		"bad status":     []byte(`{"vehicleId":"v","ts":"2026-01-01T00:00:00Z","lat":1,"lon":1,"headingDeg":0,"speed":0,"batteryPct":50,"temp":10,"status":"nope"}`),
	}
	for name, raw := range cases {
		if _, err := Parse(raw); err == nil {
			t.Errorf("%s: expected rejection, got none", name)
		}
	}
}

func TestValidateEmptyStatusAllowed(t *testing.T) {
	m := Message{VehicleID: "v", TS: time.Now(), Lat: 1, Lon: 1, HeadingDeg: 0, Speed: 0, BatteryPct: 50, Temp: 10}
	if err := m.Validate(); err != nil {
		t.Fatalf("empty status should be allowed: %v", err)
	}
}
