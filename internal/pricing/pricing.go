package pricing

// Prices are Claude Sonnet 5's standing (not introductory) per-token rate
// (platform.claude.com/docs/en/about-claude/pricing) and Voyage 3.5 Lite's
// rate (docs.voyageai.com/docs/pricing), captured 2026-09-07.
const (
	claudeInputCostPerMTok  = 2.0
	claudeOutputCostPerMTok = 10.0
	voyageCostPerMTok       = 0.02
)

func ClaudeCost(inputTokens, outputTokens int64) float64 {
	return float64(inputTokens)/1_000_000*claudeInputCostPerMTok +
		float64(outputTokens)/1_000_000*claudeOutputCostPerMTok
}

func EmbeddingCost(totalTokens int64) float64 {
	return float64(totalTokens) / 1_000_000 * voyageCostPerMTok
}
