package styleanalysis

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type Summary struct {
	VideoCountAnalyzed int    `json:"video_count_analyzed"`
	Confidence         string `json:"confidence"`

	AverageTitleLength        float64 `json:"average_title_length"`
	TitlesWithQuestionMarkPct float64 `json:"titles_with_question_mark_pct"`
	TitlesWithNumberPct       float64 `json:"titles_with_number_pct"`
	TitlesWithEmojiPct        float64 `json:"titles_with_emoji_pct"`
	TitlesWithAllCapsWordPct  float64 `json:"titles_with_all_caps_word_pct"`

	AverageTagsPerVideo float64  `json:"average_tags_per_video"`
	TopTags             []string `json:"top_tags"`

	AverageDescriptionLength      float64 `json:"average_description_length"`
	DescriptionsWithTimestampsPct float64 `json:"descriptions_with_timestamps_pct"`
	DescriptionsWithLinksPct      float64 `json:"descriptions_with_links_pct"`
	DescriptionsWithHashtagsPct   float64 `json:"descriptions_with_hashtags_pct"`
}

const lowConfidenceThreshold = 5

var (
	timestampPattern = regexp.MustCompile(`\b\d{1,2}:\d{2}(:\d{2})?\b`)
	urlPattern       = regexp.MustCompile(`https?://\S+`)
	hashtagPattern   = regexp.MustCompile(`#\w+`)
)

func Analyze(videos []storage.ChannelVideo) Summary {
	n := len(videos)
	summary := Summary{VideoCountAnalyzed: n}
	if n < lowConfidenceThreshold {
		summary.Confidence = "low"
	} else {
		summary.Confidence = "high"
	}
	if n == 0 {
		return summary
	}

	var (
		totalTitleLen     int
		questionMarkCount int
		numberCount       int
		emojiCount        int
		allCapsCount      int
		totalTags         int
		tagFrequency      = map[string]int{}
		totalDescLen      int
		timestampCount    int
		linkCount         int
		hashtagCount      int
	)

	for _, v := range videos {
		totalTitleLen += len(v.Title)
		if strings.Contains(v.Title, "?") {
			questionMarkCount++
		}
		if containsDigit(v.Title) {
			numberCount++
		}
		if containsEmoji(v.Title) {
			emojiCount++
		}
		if containsAllCapsWord(v.Title) {
			allCapsCount++
		}

		totalTags += len(v.Tags)
		for _, tag := range v.Tags {
			tagFrequency[strings.ToLower(tag)]++
		}

		totalDescLen += len(v.Description)
		if len(timestampPattern.FindAllString(v.Description, -1)) >= 2 {
			timestampCount++
		}
		if urlPattern.MatchString(v.Description) {
			linkCount++
		}
		if hashtagPattern.MatchString(v.Description) {
			hashtagCount++
		}
	}

	fn := float64(n)
	summary.AverageTitleLength = float64(totalTitleLen) / fn
	summary.TitlesWithQuestionMarkPct = percentage(questionMarkCount, n)
	summary.TitlesWithNumberPct = percentage(numberCount, n)
	summary.TitlesWithEmojiPct = percentage(emojiCount, n)
	summary.TitlesWithAllCapsWordPct = percentage(allCapsCount, n)

	summary.AverageTagsPerVideo = float64(totalTags) / fn
	summary.TopTags = topTags(tagFrequency, 10)

	summary.AverageDescriptionLength = float64(totalDescLen) / fn
	summary.DescriptionsWithTimestampsPct = percentage(timestampCount, n)
	summary.DescriptionsWithLinksPct = percentage(linkCount, n)
	summary.DescriptionsWithHashtagsPct = percentage(hashtagCount, n)

	return summary
}

func percentage(count, total int) float64 {
	return float64(count) / float64(total) * 100
}

func containsDigit(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func containsEmoji(s string) bool {
	for _, r := range s {
		if r >= 0x1F300 && r <= 0x1FAFF {
			return true
		}
	}
	return false
}

func containsAllCapsWord(s string) bool {
	for _, word := range strings.Fields(s) {
		if isAllCapsWord(word) {
			return true
		}
	}
	return false
}

func isAllCapsWord(word string) bool {
	letters := 0
	for _, r := range word {
		if unicode.IsLetter(r) {
			letters++
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return letters >= 2
}

func topTags(freq map[string]int, limit int) []string {
	type kv struct {
		tag   string
		count int
	}
	pairs := make([]kv, 0, len(freq))
	for tag, count := range freq {
		pairs = append(pairs, kv{tag, count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].tag < pairs[j].tag
	})
	if len(pairs) > limit {
		pairs = pairs[:limit]
	}
	result := make([]string, len(pairs))
	for i, p := range pairs {
		result[i] = p.tag
	}
	return result
}
