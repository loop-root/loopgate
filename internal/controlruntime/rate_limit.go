package controlruntime

import "time"

type SlidingWindowDecision struct {
	Denied     bool
	Timestamps []time.Time
}

func CheckSlidingWindowRateLimit(timestamps []time.Time, limit int, window time.Duration, nowUTC time.Time) SlidingWindowDecision {
	if limit <= 0 || window <= 0 {
		return SlidingWindowDecision{
			Denied:     false,
			Timestamps: timestamps,
		}
	}

	nowUTC = nowUTC.UTC()
	cutoff := nowUTC.Add(-window)
	pruned := make([]time.Time, 0, len(timestamps))
	for _, timestamp := range timestamps {
		if timestamp.After(cutoff) {
			pruned = append(pruned, timestamp)
		}
	}
	if len(pruned) >= limit {
		return SlidingWindowDecision{
			Denied:     true,
			Timestamps: pruned,
		}
	}
	return SlidingWindowDecision{
		Denied:     false,
		Timestamps: append(pruned, nowUTC),
	}
}
