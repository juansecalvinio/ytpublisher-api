package apikey

import (
	"strings"
	"testing"
)

func TestGenerate_HasExpectedPrefixAndLength(t *testing.T) {
	key, err := Generate()
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	if !strings.HasPrefix(key, "ytpub_") {
		t.Errorf("key = %q, want prefix %q", key, "ytpub_")
	}
	if len(key) != len("ytpub_")+48 {
		t.Errorf("len(key) = %d, want %d", len(key), len("ytpub_")+48)
	}
}

func TestGenerate_ProducesDistinctKeys(t *testing.T) {
	key1, err := Generate()
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	key2, err := Generate()
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	if key1 == key2 {
		t.Error("Generate() returned the same key twice")
	}
}

func TestHash_IsDeterministic(t *testing.T) {
	if Hash("abc") != Hash("abc") {
		t.Error("Hash() is not deterministic for the same input")
	}
}

func TestHash_DiffersForDifferentInput(t *testing.T) {
	if Hash("abc") == Hash("xyz") {
		t.Error("Hash() produced the same output for different input")
	}
}

func TestHash_MatchesKnownSHA256Vector(t *testing.T) {
	got := Hash("")
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("Hash(\"\") = %q, want %q", got, want)
	}
}
