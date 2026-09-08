package pricing

import "testing"

func TestClaudeCost(t *testing.T) {
	tests := []struct {
		name                      string
		inputTokens, outputTokens int64
		want                      float64
	}{
		{"zero tokens", 0, 0, 0},
		{"1M input tokens only", 1_000_000, 0, 2.0},
		{"1M output tokens only", 0, 1_000_000, 10.0},
		{"mixed", 500_000, 200_000, 3.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClaudeCost(tt.inputTokens, tt.outputTokens)
			if got != tt.want {
				t.Errorf("ClaudeCost(%d, %d) = %v, want %v", tt.inputTokens, tt.outputTokens, got, tt.want)
			}
		})
	}
}

func TestEmbeddingCost(t *testing.T) {
	tests := []struct {
		name        string
		totalTokens int64
		want        float64
	}{
		{"zero tokens", 0, 0},
		{"1M tokens", 1_000_000, 0.02},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EmbeddingCost(tt.totalTokens)
			if got != tt.want {
				t.Errorf("EmbeddingCost(%d) = %v, want %v", tt.totalTokens, got, tt.want)
			}
		})
	}
}
