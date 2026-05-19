package main

import (
	"sync"
	"time"
)

const (
	loginMaxFailures = 5
	loginWindow      = 10 * time.Minute
	loginMaxDelay    = 32 * time.Second
)

type loginThrottle struct {
	mu      sync.Mutex
	entries map[string]*throttleEntry
}

type throttleEntry struct {
	failures    int
	windowStart time.Time
}

func newLoginThrottle() *loginThrottle {
	lt := &loginThrottle{entries: make(map[string]*throttleEntry)}
	go lt.sweep()
	return lt
}

// isBlocked returns true when the IP has hit the failure cap within the window.
func (lt *loginThrottle) isBlocked(ip string) bool {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	e := lt.entries[ip]
	return e != nil && time.Since(e.windowStart) <= loginWindow && e.failures >= loginMaxFailures
}

// recordFailure increments the counter and returns the backoff delay to sleep.
// Delay is 2^(n-1) seconds: 1 s, 2 s, 4 s, 8 s, 16 s, capped at loginMaxDelay.
func (lt *loginThrottle) recordFailure(ip string) time.Duration {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	e := lt.entries[ip]
	if e == nil {
		e = &throttleEntry{windowStart: time.Now()}
		lt.entries[ip] = e
	} else if time.Since(e.windowStart) > loginWindow {
		e.failures = 0
		e.windowStart = time.Now()
	}
	if e.failures < loginMaxFailures {
		e.failures++
	}
	delay := time.Duration(1<<uint(e.failures-1)) * time.Second
	if delay > loginMaxDelay {
		delay = loginMaxDelay
	}
	return delay
}

// reset clears the failure counter on a successful login.
func (lt *loginThrottle) reset(ip string) {
	lt.mu.Lock()
	delete(lt.entries, ip)
	lt.mu.Unlock()
}

func (lt *loginThrottle) sweep() {
	ticker := time.NewTicker(loginWindow)
	defer ticker.Stop()
	for range ticker.C {
		lt.mu.Lock()
		now := time.Now()
		for ip, e := range lt.entries {
			if now.Sub(e.windowStart) > loginWindow {
				delete(lt.entries, ip)
			}
		}
		lt.mu.Unlock()
	}
}
