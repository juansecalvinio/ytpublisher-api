package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter_AllowsUpToMaxWithinWindow(t *testing.T) {
	l := NewLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		if !l.Allow("client-1") {
			t.Fatalf("Allow() call %d = false, want true (under max)", i+1)
		}
	}
}

func TestLimiter_RejectsOverMaxWithinWindow(t *testing.T) {
	l := NewLimiter(2, time.Minute)

	l.Allow("client-1")
	l.Allow("client-1")

	if l.Allow("client-1") {
		t.Error("Allow() 3rd call = true, want false (over max within window)")
	}
}

func TestLimiter_TracksClientsIndependently(t *testing.T) {
	l := NewLimiter(1, time.Minute)

	l.Allow("client-1")

	if !l.Allow("client-2") {
		t.Error("Allow() for a different client = false, want true (limits are per-client)")
	}
}

func TestLimiter_AllowsAgainAfterWindowExpires(t *testing.T) {
	l := NewLimiter(1, 20*time.Millisecond)

	if !l.Allow("client-1") {
		t.Fatal("first Allow() = false, want true")
	}
	if l.Allow("client-1") {
		t.Fatal("second Allow() within window = true, want false")
	}

	time.Sleep(30 * time.Millisecond)

	if !l.Allow("client-1") {
		t.Error("Allow() after window expired = false, want true")
	}
}
