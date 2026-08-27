package styleanalysis

import (
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

func TestAnalyze_EmptyVideos_ReturnsLowConfidenceZeroSummary(t *testing.T) {
	summary := Analyze(nil)

	if summary.VideoCountAnalyzed != 0 {
		t.Errorf("VideoCountAnalyzed = %d, want 0", summary.VideoCountAnalyzed)
	}
	if summary.Confidence != "low" {
		t.Errorf("Confidence = %q, want %q", summary.Confidence, "low")
	}
}

func TestAnalyze_SetsLowConfidenceBelowThreshold(t *testing.T) {
	videos := make([]storage.ChannelVideo, 4)
	for i := range videos {
		videos[i] = storage.ChannelVideo{Title: "Video"}
	}

	summary := Analyze(videos)

	if summary.Confidence != "low" {
		t.Errorf("Confidence = %q, want %q for 4 videos", summary.Confidence, "low")
	}
}

func TestAnalyze_SetsHighConfidenceAtThreshold(t *testing.T) {
	videos := make([]storage.ChannelVideo, 5)
	for i := range videos {
		videos[i] = storage.ChannelVideo{Title: "Video"}
	}

	summary := Analyze(videos)

	if summary.Confidence != "high" {
		t.Errorf("Confidence = %q, want %q for 5 videos", summary.Confidence, "high")
	}
}

func TestAnalyze_ComputesAverageTitleLength(t *testing.T) {
	videos := []storage.ChannelVideo{
		{Title: "1234567890"}, // 10 chars
		{Title: "12345"},      // 5 chars
	}

	summary := Analyze(videos)

	if summary.AverageTitleLength != 7.5 {
		t.Errorf("AverageTitleLength = %v, want 7.5", summary.AverageTitleLength)
	}
}

func TestAnalyze_DetectsQuestionMarksNumbersAndAllCapsInTitles(t *testing.T) {
	videos := []storage.ChannelVideo{
		{Title: "How to code in Go?"},
		{Title: "10 tips for beginners"},
		{Title: "THIS IS HUGE news"},
		{Title: "a plain title"},
	}

	summary := Analyze(videos)

	if summary.TitlesWithQuestionMarkPct != 25 {
		t.Errorf("TitlesWithQuestionMarkPct = %v, want 25", summary.TitlesWithQuestionMarkPct)
	}
	if summary.TitlesWithNumberPct != 25 {
		t.Errorf("TitlesWithNumberPct = %v, want 25", summary.TitlesWithNumberPct)
	}
	if summary.TitlesWithAllCapsWordPct != 25 {
		t.Errorf("TitlesWithAllCapsWordPct = %v, want 25", summary.TitlesWithAllCapsWordPct)
	}
}

func TestAnalyze_ComputesTagFrequencyAndTopTags(t *testing.T) {
	videos := []storage.ChannelVideo{
		{Tags: []string{"go", "programming"}},
		{Tags: []string{"go", "backend"}},
		{Tags: []string{"go"}},
	}

	summary := Analyze(videos)

	wantAvg := float64(2+2+1) / 3
	if summary.AverageTagsPerVideo != wantAvg {
		t.Errorf("AverageTagsPerVideo = %v, want %v", summary.AverageTagsPerVideo, wantAvg)
	}
	if len(summary.TopTags) == 0 || summary.TopTags[0] != "go" {
		t.Errorf("TopTags[0] = %v, want %q (most frequent)", summary.TopTags, "go")
	}
}

func TestAnalyze_DetectsDescriptionStructure(t *testing.T) {
	videos := []storage.ChannelVideo{
		{Description: "Intro\n0:00 Start\n1:30 Middle\n3:00 End\nCheck https://example.com and #golang"},
		{Description: "No structure here at all"},
	}

	summary := Analyze(videos)

	if summary.DescriptionsWithTimestampsPct != 50 {
		t.Errorf("DescriptionsWithTimestampsPct = %v, want 50", summary.DescriptionsWithTimestampsPct)
	}
	if summary.DescriptionsWithLinksPct != 50 {
		t.Errorf("DescriptionsWithLinksPct = %v, want 50", summary.DescriptionsWithLinksPct)
	}
	if summary.DescriptionsWithHashtagsPct != 50 {
		t.Errorf("DescriptionsWithHashtagsPct = %v, want 50", summary.DescriptionsWithHashtagsPct)
	}
}
