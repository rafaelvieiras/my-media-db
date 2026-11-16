package server

import (
	"sync"
	"time"
)

// ttlTelemetria is the TTL of the GET /api/v1/health/system cache: fresh
// enough to reflect a recent change (e.g. a scan just ran), without recalculating
// on every reload/window focus of TanStack Query. A code constant, not a preference
// exposed in the UI — same spirit as ttlCacheExterno (internal/server/search.go).
const ttlTelemetria = 60 * time.Second

// telemetryCache is an in-memory cache, protected by a mutex, of the ALREADY
// SERIALIZED telemetry snapshot (systemHealthResponse), with TTL expiration.
// Same design as cacheExterno, but SINGLETON (a single entry): this
// endpoint has no variable parameter, unlike the external search which
// caches per (query,type).
type telemetryCache struct {
	mu     sync.Mutex
	ttl    time.Duration
	item   *systemHealthResponse
	expira time.Time
	agora  func() time.Time
}

// novoTelemetryCache creates an empty cache with the given TTL.
func novoTelemetryCache(ttl time.Duration) *telemetryCache {
	return &telemetryCache{ttl: ttl, agora: time.Now}
}

// obter returns the cached snapshot and true when it exists and has not
// expired yet; otherwise (systemHealthResponse{}, false).
func (c *telemetryCache) obter() (systemHealthResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.item == nil || c.agora().After(c.expira) {
		return systemHealthResponse{}, false
	}
	return *c.item, true
}

// guardar writes (or overwrites) the snapshot with a new expiration.
func (c *telemetryCache) guardar(resp systemHealthResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item := resp
	c.item = &item
	c.expira = c.agora().Add(c.ttl)
}
