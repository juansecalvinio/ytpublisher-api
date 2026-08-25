package api

import "testing"

func TestNewRequestID_ProducesNonEmptyDistinctValues(t *testing.T) {
	id1 := NewRequestID()
	id2 := NewRequestID()

	if id1 == "" {
		t.Error("NewRequestID() returned an empty string")
	}
	if id1 == id2 {
		t.Error("NewRequestID() returned the same value twice")
	}
}
