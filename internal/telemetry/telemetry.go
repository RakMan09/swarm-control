// Package telemetry defines the vehicle telemetry message and its validation.
package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// Message is a single telemetry sample emitted by a vehicle. Field names match
// the wire schema documented in P4-SwarmControl.md section 5.
type Message struct {
	VehicleID  string    `json:"vehicleId"`
	TS         time.Time `json:"ts"`
	Lat        float64   `json:"lat"`
	Lon        float64   `json:"lon"`
	HeadingDeg float64   `json:"headingDeg"`
	Speed      float64   `json:"speed"`
	BatteryPct float64   `json:"batteryPct"`
	Temp       float64   `json:"temp"`
	Status     string    `json:"status"`
}

// ErrInvalid is returned (wrapped) when a message fails validation.
var ErrInvalid = errors.New("invalid telemetry")

// validStatuses is the closed set of vehicle statuses the platform understands.
var validStatuses = map[string]bool{
	"ok":        true,
	"active":    true,
	"idle":      true,
	"returning": true,
	"low_batt":  true,
	"fault":     true,
	"offline":   true,
}

// Parse unmarshals and validates a raw telemetry payload. Malformed or
// out-of-range messages are rejected so bad data never reaches storage.
func Parse(raw []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(raw, &m); err != nil {
		return Message{}, fmt.Errorf("%w: bad json: %v", ErrInvalid, err)
	}
	if err := m.Validate(); err != nil {
		return Message{}, err
	}
	return m, nil
}

// Validate enforces the schema constraints for a telemetry message.
func (m Message) Validate() error {
	if m.VehicleID == "" {
		return fmt.Errorf("%w: missing vehicleId", ErrInvalid)
	}
	if m.TS.IsZero() {
		return fmt.Errorf("%w: missing ts", ErrInvalid)
	}
	if math.IsNaN(m.Lat) || m.Lat < -90 || m.Lat > 90 {
		return fmt.Errorf("%w: lat out of range: %v", ErrInvalid, m.Lat)
	}
	if math.IsNaN(m.Lon) || m.Lon < -180 || m.Lon > 180 {
		return fmt.Errorf("%w: lon out of range: %v", ErrInvalid, m.Lon)
	}
	if math.IsNaN(m.HeadingDeg) || m.HeadingDeg < 0 || m.HeadingDeg >= 360 {
		return fmt.Errorf("%w: heading out of range: %v", ErrInvalid, m.HeadingDeg)
	}
	if math.IsNaN(m.Speed) || m.Speed < 0 {
		return fmt.Errorf("%w: negative speed: %v", ErrInvalid, m.Speed)
	}
	if math.IsNaN(m.BatteryPct) || m.BatteryPct < 0 || m.BatteryPct > 100 {
		return fmt.Errorf("%w: battery out of range: %v", ErrInvalid, m.BatteryPct)
	}
	if math.IsNaN(m.Temp) {
		return fmt.Errorf("%w: temp is NaN", ErrInvalid)
	}
	if m.Status != "" && !validStatuses[m.Status] {
		return fmt.Errorf("%w: unknown status %q", ErrInvalid, m.Status)
	}
	return nil
}
