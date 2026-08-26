package cache

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spy4x/oko/internal/gatus"
)

func TestCache_FreshHit(t *testing.T) {
	// Pre-populate the cache directly via store + immediate Get.
	data := map[string]gatus.Status{
		"uptime-cloud|home_a": {Healthy: ptrBool(true)},
	}
	c := New(time.Minute, func(_ context.Context, _ []string) (map[string]gatus.Status, error) {
		t.Fatal("fetcher should not be called on a fresh hit")
		return nil, nil
	})
	c.store(data)
	defer c.Stop()

	got, err := c.Get(t.Context(), []string{"uptime-cloud|home_a"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d, want 1", len(got))
	}
}

func TestCache_StaleTriggersFetch(t *testing.T) {
	var hits int32
	data := map[string]gatus.Status{
		"uptime-cloud|home_a": {Healthy: ptrBool(true)},
	}
	c := New(20*time.Millisecond, func(_ context.Context, _ []string) (map[string]gatus.Status, error) {
		atomic.AddInt32(&hits, 1)
		return data, nil
	})
	defer c.Stop()

	if _, err := c.Get(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("hits=%d, want 1", hits)
	}

	// Wait past TTL, then Get should refetch.
	time.Sleep(40 * time.Millisecond)
	if _, err := c.Get(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&hits) < 2 {
		t.Errorf("hits=%d, want >=2 after TTL", hits)
	}
}

func TestCache_SingleFlight(t *testing.T) {
	// Fetcher blocks on a channel so we can hold N callers in flight.
	hold := make(chan struct{})
	var inFlight int32
	var maxInFlight int32

	c := New(time.Minute, func(ctx context.Context, _ []string) (map[string]gatus.Status, error) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			cur := atomic.LoadInt32(&maxInFlight)
			if n <= cur || atomic.CompareAndSwapInt32(&maxInFlight, cur, n) {
				break
			}
		}
		<-hold
		atomic.AddInt32(&inFlight, -1)
		return map[string]gatus.Status{}, nil
	})
	defer c.Stop()

	const N = 10
	done := make(chan struct{}, N)
	for i := 0; i < N; i++ {
		go func() {
			_, _ = c.Get(t.Context(), nil)
			done <- struct{}{}
		}()
	}

	// Give them a moment to all pile up on the single-flight lock.
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&maxInFlight); got != 1 {
		t.Errorf("max in-flight = %d, want 1 (single-flight)", got)
	}

	close(hold)
	for i := 0; i < N; i++ {
		<-done
	}
}

func TestCache_RefreshForcesFetch(t *testing.T) {
	var hits int32
	c := New(time.Hour, func(_ context.Context, _ []string) (map[string]gatus.Status, error) {
		atomic.AddInt32(&hits, 1)
		return map[string]gatus.Status{}, nil
	})
	defer c.Stop()

	if err := c.Refresh(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Refresh(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("hits=%d, want 2", got)
	}
}

func TestCache_FetchErrorPropagated(t *testing.T) {
	want := errors.New("gatus down")
	c := New(time.Minute, func(_ context.Context, _ []string) (map[string]gatus.Status, error) {
		return nil, want
	})
	defer c.Stop()

	_, err := c.Get(t.Context(), nil)
	if !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}

func ptrBool(b bool) *bool { return &b }
