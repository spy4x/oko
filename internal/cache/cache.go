// Package cache is a single-flight TTL cache for gatus statuses.
//
// On Get, if the cache is fresh, the snapshot is returned immediately.
// If expired or empty, the first caller blocks on the fetch; concurrent
// callers block on the same fetch (single-flight). A background ticker
// re-fetches every TTL to keep the cache warm even with zero traffic.
//
// The cache remembers the key set it was last asked for. The ticker
// re-fetches that whole set, so a key whose fetch failed once is
// retried on the next tick, and a changed set (catalog edited) makes
// the snapshot stale at once.
package cache

import (
	"context"
	"slices"
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
	keys   []string // sorted; the set the snapshot was fetched for
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
	keys = sortedCopy(keys)
	if c.fresh(keys) {
		return c.snapshot(), nil
	}

	c.flight.Lock()
	defer c.flight.Unlock()

	// Re-check after acquiring the lock — a concurrent caller may have
	// just refreshed.
	if c.fresh(keys) {
		return c.snapshot(), nil
	}

	data, err := c.fetch(ctx, keys)
	if err != nil {
		return nil, err
	}
	c.store(keys, data)
	return c.snapshot(), nil
}

// Refresh forces an immediate fetch, bypassing the TTL. Used by ?refresh=1.
//
// Like Get, this is single-flight: concurrent Refresh callers share one
// in-flight fetch. The cache is updated with the result whether the call
// originated here or from the background ticker.
func (c *Cache) Refresh(ctx context.Context, keys []string) error {
	keys = sortedCopy(keys)
	c.flight.Lock()
	defer c.flight.Unlock()

	data, err := c.fetch(ctx, keys)
	if err != nil {
		return err
	}
	c.store(keys, data)
	return nil
}

// Stop terminates the background refresh loop. Idempotent.
func (c *Cache) Stop() {
	c.once.Do(func() { close(c.stop) })
}

// fresh reports whether the snapshot can be served for keys. An empty
// snapshot (every lookup failed) is fresh too: the ticker retries it, and
// refetching per request would make each visitor wait out the timeout.
func (c *Cache) fresh(keys []string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.expiry.IsZero() && time.Now().Before(c.expiry) && slices.Equal(keys, c.keys)
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

// store saves a snapshot. keys must already be sorted.
func (c *Cache) store(keys []string, data map[string]gatus.Status) {
	c.mu.Lock()
	c.data = data
	c.keys = keys
	c.expiry = time.Now().Add(c.ttl)
	c.mu.Unlock()
}

func sortedCopy(keys []string) []string {
	out := slices.Clone(keys)
	slices.Sort(out)
	return out
}

// refreshLoop re-fetches on every tick to keep the cache warm even if no
// one is hitting the page. It fetches the last requested key set, not
// the keys present in the snapshot: those are only the ones that
// succeeded, so a key that failed once would never be tried again.
// Uses a 10s hard timeout per tick to bound fetch hang time.
func (c *Cache) refreshLoop() {
	t := time.NewTicker(c.ttl)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.mu.Lock()
			keys := c.keys
			c.mu.Unlock()
			if len(keys) == 0 {
				continue
			}
			c.flight.Lock()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			data, err := c.fetch(ctx, keys)
			cancel()
			if err == nil {
				c.store(keys, data)
			} // on error keep previous snapshot — better stale than empty
			c.flight.Unlock()
		}
	}
}
