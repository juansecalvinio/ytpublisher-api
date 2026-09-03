package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const sendURL = "https://api.resend.com/emails"

type Client struct {
	apiKey     string
	from       string
	httpClient *http.Client
}

func NewClient(apiKey, from string) *Client {
	return &Client{apiKey: apiKey, from: from, httpClient: &http.Client{}}
}

type sendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

func (c *Client) SendAPIKeyEmail(ctx context.Context, toEmail, toName, apiKey string) error {
	body := sendRequest{
		From:    c.from,
		To:      []string{toEmail},
		Subject: "Your YTPublisher API key",
		Text: fmt.Sprintf(
			"Hi %s,\n\nThanks for subscribing to YTPublisher API. Here's your API key — save it now, it won't be shown again:\n\n%s\n\nUse it as a Bearer token: Authorization: Bearer %s\n",
			toName, apiKey, apiKey,
		),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("email: resend returned status %d: %s", resp.StatusCode, respBody)
	}
	return nil
}
