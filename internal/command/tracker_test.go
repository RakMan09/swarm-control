package command

import (
	"testing"
	"time"
)

func TestLifecycleSentAckedApplied(t *testing.T) {
	tr := NewTracker(nil)
	c := tr.Create("veh-1", "RTB", nil)
	if c.State != StatePending {
		t.Fatalf("new command should be pending, got %s", c.State)
	}
	if _, err := tr.MarkSent(c.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := tr.Get(c.ID); got.State != StateSent || got.SentAt == nil {
		t.Fatalf("expected sent with SentAt, got %+v", got)
	}
	if _, err := tr.Ack(c.ID, "acked"); err != nil {
		t.Fatal(err)
	}
	if got, _ := tr.Get(c.ID); got.State != StateAcked || got.AckedAt == nil {
		t.Fatalf("expected acked, got %+v", got)
	}
	if _, err := tr.Ack(c.ID, "applied"); err != nil {
		t.Fatal(err)
	}
	got, _ := tr.Get(c.ID)
	if got.State != StateApplied || got.AppliedAt == nil {
		t.Fatalf("expected applied, got %+v", got)
	}
}

func TestAppliedAckImpliesAcked(t *testing.T) {
	tr := NewTracker(nil)
	c := tr.Create("veh-1", "reroute", nil)
	tr.MarkSent(c.ID)
	tr.Ack(c.ID, "applied") // skip the intermediate acked
	got, _ := tr.Get(c.ID)
	if got.State != StateApplied || got.AckedAt == nil {
		t.Fatalf("applied ack should also set AckedAt, got %+v", got)
	}
}

func TestUndeliveredSweep(t *testing.T) {
	tr := NewTracker(nil)
	base := time.Now()
	tr.SetClock(func() time.Time { return base })
	c := tr.Create("veh-1", "RTB", nil)
	tr.MarkSent(c.ID)

	// Before timeout: nothing swept.
	if got := tr.SweepUndelivered(10 * time.Second); len(got) != 0 {
		t.Fatalf("expected no undelivered before timeout, got %d", len(got))
	}

	// Advance the clock past the timeout.
	tr.SetClock(func() time.Time { return base.Add(20 * time.Second) })
	got := tr.SweepUndelivered(10 * time.Second)
	if len(got) != 1 || got[0].State != StateUndelivered {
		t.Fatalf("expected 1 undelivered, got %+v", got)
	}
}

func TestAckedCommandNotSweptUndelivered(t *testing.T) {
	tr := NewTracker(nil)
	base := time.Now()
	tr.SetClock(func() time.Time { return base })
	c := tr.Create("veh-1", "RTB", nil)
	tr.MarkSent(c.ID)
	tr.Ack(c.ID, "acked")
	tr.SetClock(func() time.Time { return base.Add(1 * time.Hour) })
	if got := tr.SweepUndelivered(10 * time.Second); len(got) != 0 {
		t.Fatalf("acked command must not be flagged undelivered, got %d", len(got))
	}
}

func TestPersisterCalledOnTransitions(t *testing.T) {
	var count int
	tr := NewTracker(func(Command) { count++ })
	c := tr.Create("veh-1", "RTB", nil) // 1
	tr.MarkSent(c.ID)                   // 2
	tr.Ack(c.ID, "applied")             // 3
	if count < 3 {
		t.Fatalf("expected persister called >=3 times, got %d", count)
	}
}
