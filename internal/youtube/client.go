package youtube

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/option"
	youtubeapi "google.golang.org/api/youtube/v3"
)

type Video struct {
	ID          string
	Title       string
	Description string
	Tags        []string
	PublishedAt time.Time
}

type FetchResult struct {
	Videos    []Video
	QuotaUsed int
}

var ErrChannelNotFound = errors.New("youtube: channel not found")

type Client struct {
	service *youtubeapi.Service
}

func NewClient(ctx context.Context, apiKey string) (*Client, error) {
	service, err := youtubeapi.NewService(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("youtube: creating service: %w", err)
	}
	return &Client{service: service}, nil
}

func (c *Client) FetchLatestVideos(ctx context.Context, channelID string, maxResults int) (FetchResult, error) {
	var result FetchResult

	channelsResp, err := c.service.Channels.List([]string{"contentDetails"}).Id(channelID).Context(ctx).Do()
	result.QuotaUsed++
	if err != nil {
		return result, fmt.Errorf("youtube: channels.list: %w", err)
	}
	if len(channelsResp.Items) == 0 {
		return result, ErrChannelNotFound
	}
	uploadsPlaylistID := channelsResp.Items[0].ContentDetails.RelatedPlaylists.Uploads

	playlistResp, err := c.service.PlaylistItems.List([]string{"snippet"}).
		PlaylistId(uploadsPlaylistID).MaxResults(int64(maxResults)).Context(ctx).Do()
	result.QuotaUsed++
	if err != nil {
		return result, fmt.Errorf("youtube: playlistItems.list: %w", err)
	}

	videoIDs := make([]string, 0, len(playlistResp.Items))
	for _, item := range playlistResp.Items {
		videoIDs = append(videoIDs, item.Snippet.ResourceId.VideoId)
	}
	if len(videoIDs) == 0 {
		return result, nil
	}

	videosResp, err := c.service.Videos.List([]string{"snippet"}).Id(strings.Join(videoIDs, ",")).Context(ctx).Do()
	result.QuotaUsed++
	if err != nil {
		return result, fmt.Errorf("youtube: videos.list: %w", err)
	}

	for _, v := range videosResp.Items {
		publishedAt, err := time.Parse(time.RFC3339, v.Snippet.PublishedAt)
		if err != nil {
			return result, fmt.Errorf("youtube: parsing publishedAt for video %s: %w", v.Id, err)
		}
		result.Videos = append(result.Videos, Video{
			ID:          v.Id,
			Title:       v.Snippet.Title,
			Description: v.Snippet.Description,
			Tags:        v.Snippet.Tags,
			PublishedAt: publishedAt,
		})
	}
	return result, nil
}
