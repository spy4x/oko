// Package cache is a single-flight TTL cache for gatus statuses.
//
// On Get, if the cache is fresh, the snapshot is returned immediately.
// If expired or empty, the first caller blocks on the fetch; concurrent
// callers block on the same fetch (single-flight). A background ticker
// re-fetches every TTL to keep the cache warm even with zero traffic.
package cache

import (
	"context"
	"sync"
	"time"

	"github.com/spy4x/oko/internal/gatus"
)

// Fetcher returns the current status for the given keys. Implementations
// should return absent for failed fetches (the cache treats absent as
// "unknown" — see AGENTS.md).
type Fetcher func(ctx context.Context, keys []string) (map[string]gatus.Status, error)

// Cache holds the current snapshot and its expiry.
type Cache struct {
	ttl    time.Duration
	fetch  Fetcher
	mu     sync.Mutex
	data   map[string]gatus.Status
	expiry time.Time

	flight sync.Mutex // single-flight: at most one fetch in-flight at a time
	stop   chan struct{}
	once   sync.Once
}

// New constructs a Cache. The background refresh loop starts immediately.
// The first Get will fetch if no data has been populated yet (e.g. right
// after process start, before the first ticker fires).
func New(ttl time.Duration, fetch Fetcher) *Cache {
	c := &Cache{
		ttl:   ttl,
		fetch: fetch,
		data:  make(map[string]gatus.Status),
		stop:  make(chan struct{}),
	}
	go c.refreshLoop()
	return c
}

// Get returns the current snapshot. If the snapshot is stale or empty,
// blocks on a fetch (single-flight). Pass a request-scoped ctx so a
// cancelled client doesn't keep an in-flight fetch alive.
//
// The returned map is a copy; callers may mutate it freely.
func (c *Cache) Get(ctx context.Context, keys []string) (map[string]gatus.Status, error) {
	if c.fresh() {
		return c.snapshot(), nil
	}

	c.flight.Lock()
	defer c.flight.Unlock()

	// Re-check after acquiring the lock — a concurrent caller may have
	// just refreshed.
	if c.fresh() {
		return c.snapshot(), nil
	}

	data, err := c.fetch(ctx, keys)
	if err != nil {
		return nil, err
	}
	c.store(data)
	return c.snapshot(), nil
}

// Refresh forces an immediate fetch, bypassing the TTL. Used by ?refresh=1.
//
// Like Get, this is single-flight: concurrent Refresh callers share one
// in-flight fetch. The cache is updated with the result whether the call
// originated here or from the background ticker.
func (c *Cache) Refresh(ctx context.Context, keys []string) error {
	c.flight.Lock()
	defer c.flight.Unlock()

	data, err := c.fetch(ctx, keys)
	if err != nil {
		return err
	}
	c.store(data)
	return nil
}

// Stop terminates the background refresh loop. Idempotent.
func (c *Cache) Stop() {
	c.once.Do(func() { close(c.stop) })
}

func (c *Cache) fresh() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.expiry.IsZero() && time.Now().Before(c.expiry) && len(c.data) > 0
}

func (c *Cache) snapshot() map[string]gatus.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]gatus.Status, len(c.data))
	for k, v := range c.data {
		out[k] = v
	}
	return out
}

func (c *Cache) store(data map[string]gatus.Status) {
	c.mu.Lock()
	c.data = data
	c.expiry = time.Now().Add(c.ttl)
	c.mu.Unlock()
}

// refreshLoop re-fetches on every tick to keep the cache warm even if no
// one is hitting the page. Uses a 10s hard timeout per tick to bound
// fetch hang time.
func (c *Cache) refreshLoop() {
	t := time.NewTicker(c.ttl)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.mu.Lock()
			keys := make([]string, 0, len(c.data))
			for k := range c.data {
				keys = append(keys, k)
			}
			c.mu.Unlock()
			if len(keys) == 0 {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			data, err := c.fetch(ctx, keys)
			cancel()
			if err != nil {
				continue // keep previous snapshot — better stale than empty
			}
			c.store(data)
		}
	}
}
