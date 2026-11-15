package main

import (
	"testing"
	"time"
)

func TestSpeedMeterSmoothsChunkedProgressAndReachesZero(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var meter speedMeter
	meter.reset(start)
	const mib int64 = 1 << 20
	// A 2 MiB write completing once a second previously alternated between
	// 4 MiB/s and 0 because every 500 ms sample stood alone.
	for i := 1; i <= 8; i++ {
		meter.processed.Store(int64((i+1)/2) * 2 * mib)
		speed, _ := meter.sample(start.Add(time.Duration(i) * 500 * time.Millisecond))
		if speed < 2*mib || speed > 4*mib {
			t.Fatalf("sample %d: got %d B/s; want 2–4 MiB/s", i, speed)
		}
	}
	// No completed progress for a whole window must still become zero.
	for i := 9; i <= 14; i++ {
		speed, _ := meter.sample(start.Add(time.Duration(i) * 500 * time.Millisecond))
		if i == 14 && speed != 0 {
			t.Fatalf("stalled transfer still shows %d B/s", speed)
		}
	}
}

func TestSpeedMeterUsesElapsedTimeAndResets(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var meter speedMeter
	meter.reset(start)
	meter.processed.Store(6 << 20)
	speed, beforeReset := meter.sample(start.Add(2 * time.Second))
	if speed != 3<<20 {
		t.Fatalf("irregular interval: got %d B/s", speed)
	}
	meter.reset(start.Add(10 * time.Second))
	if meter.generation.Load() == beforeReset {
		t.Fatal("reset did not invalidate old samples")
	}
	meter.processed.Store(8 << 20)
	if speed, _ = meter.sample(start.Add(11 * time.Second)); speed != 2<<20 {
		t.Fatalf("after reset: got %d B/s", speed)
	}
	// If the counter ever resets, do not manufacture a negative rate.
	meter.processed.Store(0)
	if speed, _ = meter.sample(start.Add(12 * time.Second)); speed != 0 {
		t.Fatalf("counter reset: got %d B/s", speed)
	}
}
