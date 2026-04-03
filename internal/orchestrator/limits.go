package orchestrator

import (
	"fmt"
	"sync"
	"time"
)

// RateLimiter enforces tool call limits per the threat model.
type RateLimiter struct {
	mu sync.Mutex

	// Configuration
	MaxCallsPerTurn    int
	MaxCallsPerSession int
	CooldownBetween    time.Duration

	// State
	turnCalls    int
	sessionCalls int
	lastCallTime time.Time
}

// NewRateLimiter creates a rate limiter with the given limits.
// Zero values disable the corresponding limit.
func NewRateLimiter(maxPerTurn, maxPerSession int, cooldown time.Duration) *RateLimiter {
	return &RateLimiter{
		MaxCallsPerTurn:    maxPerTurn,
		MaxCallsPerSession: maxPerSession,
		CooldownBetween:    cooldown,
	}
}

// DefaultRateLimiter returns a rate limiter with sensible defaults.
func DefaultRateLimiter() *RateLimiter {
	return &RateLimiter{
		MaxCallsPerTurn:    10,  // Max 10 tool calls per model turn
		MaxCallsPerSession: 500, // Max 500 tool calls per session
		CooldownBetween:    0,   // No cooldown by default
	}
}

// LimitResult contains the outcome of a rate limit check.
type LimitResult struct {
	Allowed bool
	Reason  string
}

// Check returns whether a tool call is allowed under current limits.
func (r *RateLimiter) Check() LimitResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check session limit
	if r.MaxCallsPerSession > 0 && r.sessionCalls >= r.MaxCallsPerSession {
		return LimitResult{
			Allowed: false,
			Reason:  fmt.Sprintf("session tool call limit reached (%d)", r.MaxCallsPerSession),
		}
	}

	// Check turn limit
	if r.MaxCallsPerTurn > 0 && r.turnCalls >= r.MaxCallsPerTurn {
		return LimitResult{
			Allowed: false,
			Reason:  fmt.Sprintf("turn tool call limit reached (%d)", r.MaxCallsPerTurn),
		}
	}

	// Check cooldown
	if r.CooldownBetween > 0 {
		elapsed := time.Since(r.lastCallTime)
		if elapsed < r.CooldownBetween {
			return LimitResult{
				Allowed: false,
				Reason:  fmt.Sprintf("cooldown not elapsed (wait %v)", r.CooldownBetween-elapsed),
			}
		}
	}

	return LimitResult{Allowed: true}
}

// RecordCall records that a tool call was made.
func (r *RateLimiter) RecordCall() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.turnCalls++
	r.sessionCalls++
	r.lastCallTime = time.Now()
}

// NewTurn resets the per-turn counter.
// Call this at the start of each model turn.
func (r *RateLimiter) NewTurn() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.turnCalls = 0
}

// Stats returns current usage statistics.
func (r *RateLimiter) Stats() (turnCalls, sessionCalls int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.turnCalls, r.sessionCalls
}

// Reset resets all counters (for testing or session reset).
func (r *RateLimiter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.turnCalls = 0
	r.sessionCalls = 0
	r.lastCallTime = time.Time{}
}
