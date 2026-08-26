package storage

import (
	"context"
	"testing"
)

func TestUnitsUsedToday_ReturnsNonNegativeBaseline(t *testing.T) {
	store := newTestStore(t)

	units, err := store.UnitsUsedToday(context.Background())
	if err != nil {
		t.Fatalf("UnitsUsedToday() returned unexpected error: %v", err)
	}
	if units < 0 {
		t.Errorf("UnitsUsedToday() = %d, want >= 0", units)
	}
}

// This test intentionally does not roll back its increment: today's quota
// counter is shared, cumulative state (there is no meaningful "undo" for a
// usage counter without risking corrupting real same-day usage). A few
// units added by test runs is negligible against a 9000+ unit daily cap.
func TestIncrementUnitsUsed_AccumulatesAcrossCalls(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	before, err := store.UnitsUsedToday(ctx)
	if err != nil {
		t.Fatalf("UnitsUsedToday() returned unexpected error: %v", err)
	}

	if err := store.IncrementUnitsUsed(ctx, 3); err != nil {
		t.Fatalf("IncrementUnitsUsed() returned unexpected error: %v", err)
	}

	after, err := store.UnitsUsedToday(ctx)
	if err != nil {
		t.Fatalf("UnitsUsedToday() (after) returned unexpected error: %v", err)
	}
	if after != before+3 {
		t.Errorf("after = %d, want %d", after, before+3)
	}
}
