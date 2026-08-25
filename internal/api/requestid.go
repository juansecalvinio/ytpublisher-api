package api

import "crypto/rand"

// NewRequestID returns a short random identifier for correlating a
// request's usage tracking.
func NewRequestID() string {
	return rand.Text()
}
