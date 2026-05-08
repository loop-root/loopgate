package controlruntime

import (
	"testing"
	"time"
)

func TestCheckSlidingWindowRateLimit_AllowsBelowLimitAndAppendsNow(t *testing.T) {
	nowUTC := time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC)
	existing := []time.Time{nowUTC.Add(-30 * time.Second)}

	decision := CheckSlidingWindowRateLimit(existing, 2, time.Minute, nowUTC)
	if decision.Denied {
		t.Fatal("expected request below the limit to be allowed")
	}
	if len(decision.Timestamps) != 2 {
		t.Fatalf("expected two timestamps after append, got %#v", decision.Timestamps)
	}
	if !decision.Timestamps[1].Equal(nowUTC) {
		t.Fatalf("expected now timestamp to be appended, got %s", decision.Timestamps[1])
	}
}

func TestCheckSlidingWindowRateLimit_DeniesAtLimitWithoutAppending(t *testing.T) {
	nowUTC := time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC)
	existing := []time.Time{
		nowUTC.Add(-30 * time.Second),
		nowUTC.Add(-10 * time.Second),
	}

	decision := CheckSlidingWindowRateLimit(existing, 2, time.Minute, nowUTC)
	if !decision.Denied {
		t.Fatal("expected request at the limit to be denied")
	}
	if len(decision.Timestamps) != 2 {
		t.Fatalf("denied request should not append now, got %#v", decision.Timestamps)
	}
}

func TestCheckSlidingWindowRateLimit_PrunesCutoffAndOlderTimestamps(t *testing.T) {
	nowUTC := time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC)
	window := time.Minute
	expired := nowUTC.Add(-window - time.Nanosecond)
	atCutoff := nowUTC.Add(-window)
	retained := nowUTC.Add(-window + time.Nanosecond)
	existing := []time.Time{expired, atCutoff, retained}

	decision := CheckSlidingWindowRateLimit(existing, 3, window, nowUTC)
	if decision.Denied {
		t.Fatal("expected request after pruning to be allowed")
	}
	if len(decision.Timestamps) != 2 {
		t.Fatalf("expected retained timestamp plus now, got %#v", decision.Timestamps)
	}
	if !decision.Timestamps[0].Equal(retained) {
		t.Fatalf("expected only timestamp after cutoff to survive, got %#v", decision.Timestamps)
	}
}

func TestCheckSlidingWindowRateLimit_DoesNotMutateCallerBackingArray(t *testing.T) {
	nowUTC := time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC)
	existing := []time.Time{
		nowUTC.Add(-2 * time.Minute),
		nowUTC.Add(-30 * time.Second),
	}
	original := append([]time.Time(nil), existing...)

	_ = CheckSlidingWindowRateLimit(existing, 2, time.Minute, nowUTC)
	for index, expectedTimestamp := range original {
		if !existing[index].Equal(expectedTimestamp) {
			t.Fatalf("expected caller slice to remain unchanged at index %d: want %s got %s", index, expectedTimestamp, existing[index])
		}
	}
}

func TestCheckSlidingWindowRateLimit_DisabledLimitReturnsOriginalTimestamps(t *testing.T) {
	nowUTC := time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC)
	existing := []time.Time{nowUTC.Add(-30 * time.Second)}

	decision := CheckSlidingWindowRateLimit(existing, 0, time.Minute, nowUTC)
	if decision.Denied {
		t.Fatal("expected disabled limit to allow")
	}
	if len(decision.Timestamps) != len(existing) || !decision.Timestamps[0].Equal(existing[0]) {
		t.Fatalf("expected disabled limit to return original timestamps, got %#v", decision.Timestamps)
	}
}
