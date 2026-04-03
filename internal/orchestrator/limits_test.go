package orchestrator

import (
	"testing"
	"time"
)

func TestRateLimiter_TurnLimit(t *testing.T) {
	limiter := NewRateLimiter(3, 0, 0) // 3 per turn, no session limit

	// First 3 calls should be allowed
	for i := 0; i < 3; i++ {
		result := limiter.Check()
		if !result.Allowed {
			t.Errorf("call %d should be allowed", i)
		}
		limiter.RecordCall()
	}

	// 4th call should be denied
	result := limiter.Check()
	if result.Allowed {
		t.Error("4th call should be denied (turn limit)")
	}
	if result.Reason == "" {
		t.Error("expected denial reason")
	}

	// After NewTurn, should be allowed again
	limiter.NewTurn()
	result = limiter.Check()
	if !result.Allowed {
		t.Error("call after NewTurn should be allowed")
	}
}

func TestRateLimiter_SessionLimit(t *testing.T) {
	limiter := NewRateLimiter(0, 5, 0) // No turn limit, 5 per session

	// First 5 calls should be allowed
	for i := 0; i < 5; i++ {
		result := limiter.Check()
		if !result.Allowed {
			t.Errorf("call %d should be allowed", i)
		}
		limiter.RecordCall()
	}

	// 6th call should be denied
	result := limiter.Check()
	if result.Allowed {
		t.Error("6th call should be denied (session limit)")
	}

	// NewTurn should not reset session limit
	limiter.NewTurn()
	result = limiter.Check()
	if result.Allowed {
		t.Error("session limit should persist across turns")
	}

	// Reset should clear session limit
	limiter.Reset()
	result = limiter.Check()
	if !result.Allowed {
		t.Error("call after Reset should be allowed")
	}
}

func TestRateLimiter_Cooldown(t *testing.T) {
	limiter := NewRateLimiter(0, 0, 50*time.Millisecond) // 50ms cooldown

	// First call should be allowed
	result := limiter.Check()
	if !result.Allowed {
		t.Error("first call should be allowed")
	}
	limiter.RecordCall()

	// Immediate second call should be denied
	result = limiter.Check()
	if result.Allowed {
		t.Error("immediate second call should be denied (cooldown)")
	}

	// Wait for cooldown
	time.Sleep(60 * time.Millisecond)

	// Now should be allowed
	result = limiter.Check()
	if !result.Allowed {
		t.Error("call after cooldown should be allowed")
	}
}

func TestRateLimiter_CombinedLimits(t *testing.T) {
	limiter := NewRateLimiter(2, 5, 0) // 2 per turn, 5 per session

	// Turn 1: 2 calls
	limiter.RecordCall()
	limiter.RecordCall()

	result := limiter.Check()
	if result.Allowed {
		t.Error("3rd call in turn should be denied")
	}

	// Turn 2: 2 more calls (4 total)
	limiter.NewTurn()
	limiter.RecordCall()
	limiter.RecordCall()

	// Turn 3: 1 call brings us to 5
	limiter.NewTurn()
	result = limiter.Check()
	if !result.Allowed {
		t.Error("5th call should be allowed")
	}
	limiter.RecordCall()

	// 6th call should hit session limit
	result = limiter.Check()
	if result.Allowed {
		t.Error("6th call should be denied (session limit)")
	}
}

func TestRateLimiter_Stats(t *testing.T) {
	limiter := NewRateLimiter(10, 100, 0)

	limiter.RecordCall()
	limiter.RecordCall()
	limiter.RecordCall()

	turn, session := limiter.Stats()
	if turn != 3 {
		t.Errorf("expected turn=3, got %d", turn)
	}
	if session != 3 {
		t.Errorf("expected session=3, got %d", session)
	}

	limiter.NewTurn()
	limiter.RecordCall()

	turn, session = limiter.Stats()
	if turn != 1 {
		t.Errorf("expected turn=1 after NewTurn, got %d", turn)
	}
	if session != 4 {
		t.Errorf("expected session=4, got %d", session)
	}
}

func TestRateLimiter_ZeroLimitsDisabled(t *testing.T) {
	limiter := NewRateLimiter(0, 0, 0) // All limits disabled

	// Should allow unlimited calls
	for i := 0; i < 100; i++ {
		result := limiter.Check()
		if !result.Allowed {
			t.Errorf("call %d should be allowed with no limits", i)
		}
		limiter.RecordCall()
	}
}

func TestDefaultRateLimiter(t *testing.T) {
	limiter := DefaultRateLimiter()

	if limiter.MaxCallsPerTurn != 10 {
		t.Errorf("expected MaxCallsPerTurn=10, got %d", limiter.MaxCallsPerTurn)
	}
	if limiter.MaxCallsPerSession != 500 {
		t.Errorf("expected MaxCallsPerSession=500, got %d", limiter.MaxCallsPerSession)
	}
}
