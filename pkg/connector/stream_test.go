package connector

import (
	"context"
	"errors"
	"testing"
)

func TestStreamReportsError(t *testing.T) {
	ctx, sink := WithErrorSink(context.Background())
	boom := errors.New("boom")
	ch := Stream(ctx, 1, func(ctx context.Context, emit func(int) bool) error {
		emit(1)
		emit(2)
		return boom
	})
	got := Collect(ch)
	if len(got) != 2 {
		t.Fatalf("want 2 items, got %d", len(got))
	}
	if !errors.Is(sink.Err(), boom) {
		t.Fatalf("sink must hold streaming error, got %v", sink.Err())
	}
}

func TestStreamCancelled(t *testing.T) {
	ctx, sink := WithErrorSink(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	ch := Stream(ctx, 0, func(ctx context.Context, emit func(int) bool) error {
		for i := 0; ; i++ {
			if !emit(i) {
				return nil
			}
		}
	})
	<-ch
	cancel()
	for range ch {
	}
	if !errors.Is(sink.Err(), context.Canceled) {
		t.Fatalf("cancelled stream must be reported as partial, got %v", sink.Err())
	}
}

func TestConfigRequire(t *testing.T) {
	cfg := Config{Settings: map[string]string{"url": "x"}, Secrets: map[string]string{"password": "p"}}
	if err := cfg.Require("url", "password"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Require("url", "token"); !errors.Is(err, ErrMissingConfig) {
		t.Fatalf("want ErrMissingConfig, got %v", err)
	}
}
