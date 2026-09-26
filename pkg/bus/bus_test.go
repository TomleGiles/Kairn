package bus

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestMemoryBusGroupsAndDecoding(t *testing.T) {
	m := NewMemory(nil)
	var a, b atomic.Int32
	var got struct{ Period string }
	_ = m.Subscribe(SubjectReport, "reports", func(_ context.Context, msg Message) error {
		a.Add(1)
		if msg.OrgID != "org-1" {
			t.Errorf("org: %s", msg.OrgID)
		}
		return msg.Decode(&got)
	})
	_ = m.Subscribe(SubjectReport, "audit", func(context.Context, Message) error { b.Add(1); return errors.New("ignored") })
	// Un second abonnement du même groupe remplace le premier (un seul consommateur par groupe).
	_ = m.Subscribe(SubjectReport, "audit", func(context.Context, Message) error { b.Add(10); return nil })
	if err := m.Publish(context.Background(), SubjectReport, "org-1", map[string]string{"period": "2026-08"}); err != nil {
		t.Fatal(err)
	}
	m.Wait()
	if a.Load() != 1 || b.Load() != 10 || got.Period != "2026-08" {
		t.Fatalf("delivery: a=%d b=%d period=%q", a.Load(), b.Load(), got.Period)
	}
	if err := m.Publish(context.Background(), "nobody.listens", "org-1", nil); err != nil {
		t.Fatal(err)
	}
	m.Sync = true
	_ = m.Publish(context.Background(), SubjectReport, "org-1", map[string]string{"period": "2026-09"})
	if got.Period != "2026-09" {
		t.Fatal("synchronous delivery")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Publish(context.Background(), SubjectReport, "org-1", nil); err == nil {
		t.Fatal("publish after close must fail")
	}
	if _, err := NewMessage("s", "o", func() {}); err == nil {
		t.Fatal("unencodable payload must fail")
	}
}
