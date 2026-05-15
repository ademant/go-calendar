package main

import (
	"sync"
	"time"
)

// credCacheTTL caps how long a validated credential stays cached.
// Role changes and API key deletions take effect within this window.
const credCacheTTL = 5 * time.Minute

type credEntry struct {
	userID    int
	userRole  string
	expiresAt time.Time
}

type credCache struct {
	mu      sync.RWMutex
	entries map[string]credEntry
}

func newCredCache() *credCache {
	c := &credCache{entries: make(map[string]credEntry)}
	go c.sweepLoop()
	return c
}

// credentials is the process-global auth cache shared by token and API key validation.
var credentials = newCredCache()

// get returns a cached entry if it exists and has not expired.
func (c *credCache) get(key string) (userID int, role string, ok bool) {
	c.mu.RLock()
	e, found := c.entries[key]
	c.mu.RUnlock()
	if !found || time.Now().After(e.expiresAt) {
		return 0, "", false
	}
	return e.userID, e.userRole, true
}

// set stores a credential. tokenExpiry is the real token expiry from the DB;
// pass time.Time{} for API keys (no natural expiry) to use the TTL cap only.
func (c *credCache) set(key string, userID int, role string, tokenExpiry time.Time) {
	exp := time.Now().Add(credCacheTTL)
	if !tokenExpiry.IsZero() && tokenExpiry.Before(exp) {
		exp = tokenExpiry
	}
	c.mu.Lock()
	c.entries[key] = credEntry{userID: userID, userRole: role, expiresAt: exp}
	c.mu.Unlock()
}

// invalidate removes a single entry immediately (e.g. on API key deletion).
func (c *credCache) invalidate(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

func (c *credCache) sweepLoop() {
	ticker := time.NewTicker(credCacheTTL)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		c.mu.Lock()
		for k, e := range c.entries {
			if now.After(e.expiresAt) {
				delete(c.entries, k)
			}
		}
		c.mu.Unlock()
	}
}
