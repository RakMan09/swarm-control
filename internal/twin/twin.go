// Package twin maintains the per-vehicle digital twin (latest known state) in
// Redis, giving the API O(1) "current state" reads without touching Timescale.
package twin

import (
	"context"
	"strconv"
	"time"

	"github.com/rakman09/swarm-control/internal/telemetry"
	"github.com/redis/go-redis/v9"
)

const vehicleSetKey = "fleet:vehicles"

// State is the digital-twin snapshot for one vehicle.
type State struct {
	VehicleID  string  `json:"vehicleId"`
	TS         int64   `json:"ts"` // telemetry timestamp, unix ms
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	HeadingDeg float64 `json:"headingDeg"`
	Speed      float64 `json:"speed"`
	BatteryPct float64 `json:"batteryPct"`
	Temp       float64 `json:"temp"`
	Status     string  `json:"status"`
	LastSeen   int64   `json:"lastSeen"` // server receive time, unix ms
}

// Store wraps a Redis client for twin reads/writes.
type Store struct {
	rdb *redis.Client
}

// New builds a twin store from a Redis address.
func New(addr string) *Store {
	return &Store{rdb: redis.NewClient(&redis.Options{Addr: addr})}
}

// NewWithClient wraps an existing client (useful for sharing/testing).
func NewWithClient(rdb *redis.Client) *Store { return &Store{rdb: rdb} }

// Client exposes the underlying Redis client for pub/sub reuse.
func (s *Store) Client() *redis.Client { return s.rdb }

// Ping verifies connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.rdb.Ping(ctx).Err() }

func key(id string) string { return "twin:" + id }

// Update writes the latest state for a vehicle and registers it in the fleet set.
func (s *Store) Update(ctx context.Context, m telemetry.Message) error {
	now := time.Now().UnixMilli()
	k := key(m.VehicleID)
	pipe := s.rdb.Pipeline()
	pipe.HSet(ctx, k, map[string]any{
		"vehicleId":  m.VehicleID,
		"ts":         m.TS.UnixMilli(),
		"lat":        m.Lat,
		"lon":        m.Lon,
		"headingDeg": m.HeadingDeg,
		"speed":      m.Speed,
		"batteryPct": m.BatteryPct,
		"temp":       m.Temp,
		"status":     m.Status,
		"lastSeen":   now,
	})
	pipe.SAdd(ctx, vehicleSetKey, m.VehicleID)
	_, err := pipe.Exec(ctx)
	return err
}

// Get returns the twin for a single vehicle.
func (s *Store) Get(ctx context.Context, id string) (State, error) {
	h, err := s.rdb.HGetAll(ctx, key(id)).Result()
	if err != nil {
		return State{}, err
	}
	return fromHash(h), nil
}

// Snapshot returns the current twin for every registered vehicle.
func (s *Store) Snapshot(ctx context.Context) ([]State, error) {
	ids, err := s.rdb.SMembers(ctx, vehicleSetKey).Result()
	if err != nil {
		return nil, err
	}
	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.HGetAll(ctx, key(id))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}
	out := make([]State, 0, len(ids))
	for _, c := range cmds {
		h, err := c.Result()
		if err != nil || len(h) == 0 {
			continue
		}
		out = append(out, fromHash(h))
	}
	return out, nil
}

// VehicleIDs returns the set of known vehicle IDs.
func (s *Store) VehicleIDs(ctx context.Context) ([]string, error) {
	return s.rdb.SMembers(ctx, vehicleSetKey).Result()
}

func fromHash(h map[string]string) State {
	return State{
		VehicleID:  h["vehicleId"],
		TS:         atoi(h["ts"]),
		Lat:        atof(h["lat"]),
		Lon:        atof(h["lon"]),
		HeadingDeg: atof(h["headingDeg"]),
		Speed:      atof(h["speed"]),
		BatteryPct: atof(h["batteryPct"]),
		Temp:       atof(h["temp"]),
		Status:     h["status"],
		LastSeen:   atoi(h["lastSeen"]),
	}
}

func atof(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }
func atoi(s string) int64   { n, _ := strconv.ParseInt(s, 10, 64); return n }
