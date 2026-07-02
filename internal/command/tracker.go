// Package command implements the command/ack lifecycle tracker: a command moves
// pending -> sent -> acked -> applied, or is flagged undelivered when no ack
// arrives before a timeout.
package command

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

// State is a command lifecycle state.
type State string

const (
	StatePending     State = "pending"
	StateSent        State = "sent"
	StateAcked       State = "acked"
	StateApplied     State = "applied"
	StateUndelivered State = "undelivered"
)

// Command is a single command and its lifecycle timestamps.
type Command struct {
	ID        string          `json:"id"`
	VehicleID string          `json:"vehicleId"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	State     State           `json:"state"`
	CreatedAt time.Time       `json:"createdAt"`
	SentAt    *time.Time      `json:"sentAt,omitempty"`
	AckedAt   *time.Time      `json:"ackedAt,omitempty"`
	AppliedAt *time.Time      `json:"appliedAt,omitempty"`
}

// ErrNotFound is returned when a command id is unknown.
var ErrNotFound = errors.New("command not found")

// Persister receives a snapshot of a command after each transition so it can be
// written to durable storage (e.g. the command_log table). It may be nil.
type Persister func(Command)

// Tracker is an in-memory, concurrency-safe command state machine.
type Tracker struct {
	mu      sync.Mutex
	cmds    map[string]*Command
	persist Persister
	now     func() time.Time // injectable clock for tests
	seq     int64
}

// NewTracker creates a tracker. persist may be nil.
func NewTracker(persist Persister) *Tracker {
	return &Tracker{cmds: map[string]*Command{}, persist: persist, now: time.Now}
}

// SetClock overrides the clock (tests only).
func (t *Tracker) SetClock(f func() time.Time) { t.now = f }

// Create registers a new pending command and returns a copy.
func (t *Tracker) Create(vehicleID, typ string, payload json.RawMessage) Command {
	t.mu.Lock()
	t.seq++
	id := t.newID()
	c := &Command{
		ID:        id,
		VehicleID: vehicleID,
		Type:      typ,
		Payload:   payload,
		State:     StatePending,
		CreatedAt: t.now(),
	}
	t.cmds[id] = c
	snap := *c
	t.mu.Unlock()
	t.emit(snap)
	return snap
}

// MarkSent records that the command was published to the vehicle.
func (t *Tracker) MarkSent(id string) (Command, error) {
	return t.update(id, func(c *Command) {
		if c.State == StatePending {
			c.State = StateSent
			now := t.now()
			c.SentAt = &now
		}
	})
}

// Ack applies an acknowledgement. ackState must be "acked" or "applied";
// "applied" implies the vehicle both received and executed the command.
func (t *Tracker) Ack(id, ackState string) (Command, error) {
	return t.update(id, func(c *Command) {
		now := t.now()
		switch ackState {
		case "applied":
			if c.AckedAt == nil {
				c.AckedAt = &now
			}
			c.AppliedAt = &now
			c.State = StateApplied
		default: // "acked" or anything else treated as delivery ack
			if c.State != StateApplied {
				c.State = StateAcked
			}
			if c.AckedAt == nil {
				c.AckedAt = &now
			}
		}
	})
}

// SweepUndelivered flags commands still awaiting an ack past the timeout and
// returns the ones newly marked undelivered.
func (t *Tracker) SweepUndelivered(timeout time.Duration) []Command {
	var out []Command
	t.mu.Lock()
	now := t.now()
	for _, c := range t.cmds {
		if (c.State == StateSent || c.State == StatePending) && c.SentAt != nil &&
			now.Sub(*c.SentAt) > timeout {
			c.State = StateUndelivered
			out = append(out, *c)
		}
	}
	t.mu.Unlock()
	for _, c := range out {
		t.emit(c)
	}
	return out
}

// Get returns a command by id.
func (t *Tracker) Get(id string) (Command, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.cmds[id]
	if !ok {
		return Command{}, ErrNotFound
	}
	return *c, nil
}

// List returns all commands, optionally filtered by vehicle, newest first.
func (t *Tracker) List(vehicleID string) []Command {
	t.mu.Lock()
	out := make([]Command, 0, len(t.cmds))
	for _, c := range t.cmds {
		if vehicleID == "" || c.VehicleID == vehicleID {
			out = append(out, *c)
		}
	}
	t.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (t *Tracker) update(id string, fn func(*Command)) (Command, error) {
	t.mu.Lock()
	c, ok := t.cmds[id]
	if !ok {
		t.mu.Unlock()
		return Command{}, ErrNotFound
	}
	fn(c)
	snap := *c
	t.mu.Unlock()
	t.emit(snap)
	return snap, nil
}

func (t *Tracker) emit(c Command) {
	if t.persist != nil {
		t.persist(c)
	}
}

func (t *Tracker) newID() string {
	return "cmd-" + time.Now().Format("20060102T150405") + "-" + itoa(t.seq)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
