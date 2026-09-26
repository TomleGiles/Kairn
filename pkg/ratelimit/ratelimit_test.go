package ratelimit

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func allowed(l Limiter, key string, n int) int {
	ok := 0
	for range n {
		if l.Allow(context.Background(), key) {
			ok++
		}
	}
	return ok
}

func TestMemoryBucket(t *testing.T) {
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	l := NewMemory(2, 5)
	l.now = c.now
	if got := allowed(l, "a", 10); got != 5 {
		t.Fatalf("burst: got %d, want 5", got)
	}
	if got := allowed(l, "b", 1); got != 1 {
		t.Fatal("keys are independent")
	}
	c.advance(time.Second)
	if got := allowed(l, "a", 10); got != 2 {
		t.Fatalf("refill at 2/s: got %d", got)
	}
	c.advance(time.Hour)
	if got := allowed(l, "a", 10); got != 5 {
		t.Fatalf("refill is capped at burst: got %d", got)
	}
}

func TestRedisBucketIsShared(t *testing.T) {
	mr := miniredis.RunT(t)
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	newReplica := func() *Redis {
		l := NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), 2, 5, slog.New(slog.NewTextHandler(io.Discard, nil)))
		l.now = c.now
		return l
	}
	r1, r2 := newReplica(), newReplica()
	// Deux réplicas partagent le même seau : 5 requêtes au total, pas 10.
	if got := allowed(r1, "user:u1", 3) + allowed(r2, "user:u1", 3); got != 5 {
		t.Fatalf("shared burst: got %d, want 5", got)
	}
	c.advance(1500 * time.Millisecond)
	if got := allowed(r2, "user:u1", 10); got != 3 {
		t.Fatalf("shared refill (1.5 s × 2/s): got %d, want 3", got)
	}
	if ttl := mr.TTL("kairn:rl:user:u1"); ttl <= 0 || ttl > 5*time.Second {
		t.Fatalf("idle buckets must expire: ttl %s", ttl)
	}
}

func TestRedisFallback(t *testing.T) {
	mr := miniredis.RunT(t)
	l := NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1}), 1, 2, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mr.Close()
	// Redis indisponible : le seau local prend le relais (ni blocage total, ni ouverture illimitée).
	if got := allowed(l, "ip:1.2.3.4", 5); got != 2 {
		t.Fatalf("local fallback: got %d, want 2", got)
	}
}

func TestOpen(t *testing.T) {
	l, closeFn, err := Open("", 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	closeFn()
	if _, ok := l.(*Memory); !ok {
		t.Fatalf("no URL: local limiter expected, got %T", l)
	}
	mr := miniredis.RunT(t)
	l, closeFn, err = Open("redis://"+mr.Addr()+"/0", 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	if _, ok := l.(*Redis); !ok || !l.Allow(context.Background(), "k") {
		t.Fatalf("redis limiter expected, got %T", l)
	}
	if _, _, err := Open("://bad", 1, 1, nil); err == nil {
		t.Fatal("invalid URL must be rejected")
	}
}
