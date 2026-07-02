// Package bus is a thin Redis pub/sub layer connecting the services: ingestion
// fans validated telemetry out to the alert engine, and the alert engine fans
// alerts out to the API/dashboard.
package bus

import (
	"context"
	"encoding/json"

	"github.com/rakman09/swarm-control/internal/telemetry"
	"github.com/redis/go-redis/v9"
)

const (
	// ChanTelemetry carries validated telemetry messages.
	ChanTelemetry = "bus:telemetry"
	// ChanAlerts carries fired alerts.
	ChanAlerts = "bus:alerts"
)

// Alert is a fired alert event delivered to operators.
type Alert struct {
	ID         string  `json:"id"`
	VehicleID  string  `json:"vehicleId"`
	Type       string  `json:"type"`     // geofence | low_battery | lost_comms | anomaly
	Severity   string  `json:"severity"` // info | warning | critical
	Message    string  `json:"message"`
	Value      float64 `json:"value,omitempty"`
	Lat        float64 `json:"lat,omitempty"`
	Lon        float64 `json:"lon,omitempty"`
	TS         int64   `json:"ts"`         // event time, unix ms
	DetectedAt int64   `json:"detectedAt"` // detection time, unix ms
}

// Bus wraps a Redis client for typed publish/subscribe.
type Bus struct{ rdb *redis.Client }

// New builds a bus from a Redis address.
func New(addr string) *Bus {
	return &Bus{rdb: redis.NewClient(&redis.Options{Addr: addr})}
}

// NewWithClient wraps an existing Redis client.
func NewWithClient(rdb *redis.Client) *Bus { return &Bus{rdb: rdb} }

// Ping verifies connectivity.
func (b *Bus) Ping(ctx context.Context) error { return b.rdb.Ping(ctx).Err() }

// PublishTelemetry broadcasts a validated telemetry message.
func (b *Bus) PublishTelemetry(ctx context.Context, m telemetry.Message) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, ChanTelemetry, raw).Err()
}

// SubscribeTelemetry returns a channel of validated telemetry messages.
func (b *Bus) SubscribeTelemetry(ctx context.Context) <-chan telemetry.Message {
	out := make(chan telemetry.Message, 1024)
	sub := b.rdb.Subscribe(ctx, ChanTelemetry)
	go func() {
		defer close(out)
		ch := sub.Channel(redis.WithChannelSize(1024))
		for {
			select {
			case <-ctx.Done():
				_ = sub.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var m telemetry.Message
				if err := json.Unmarshal([]byte(msg.Payload), &m); err == nil {
					select {
					case out <- m:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return out
}

// PublishAlert broadcasts a fired alert.
func (b *Bus) PublishAlert(ctx context.Context, a Alert) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, ChanAlerts, raw).Err()
}

// SubscribeAlerts returns a channel of fired alerts.
func (b *Bus) SubscribeAlerts(ctx context.Context) <-chan Alert {
	out := make(chan Alert, 256)
	sub := b.rdb.Subscribe(ctx, ChanAlerts)
	go func() {
		defer close(out)
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				_ = sub.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var a Alert
				if err := json.Unmarshal([]byte(msg.Payload), &a); err == nil {
					select {
					case out <- a:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return out
}
