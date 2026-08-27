package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const embeddingsURL = "https://api.voyageai.com/v1/embeddings"

type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(apiKey, model string) *Client {
	return &Client{apiKey: apiKey, model: model, httpClient: &http.Client{}}
}

func (c *Client) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return c.embed(ctx, texts, "document")
}

func (c *Client) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vectors, err := c.embed(ctx, []string{text}, "query")
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

type embedRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (c *Client) embed(ctx context.Context, texts []string, inputType string) ([][]float32, error) {
	reqBody, err := json.Marshal(embedRequest{Input: texts, Model: c.model, InputType: inputType})
	if err != nil {
		return nil, fmt.Errorf("embeddings: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, embeddingsURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("embeddings: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embeddings: voyage returned status %d: %s", resp.StatusCode, body)
	}

	var parsed embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("embeddings: decoding response: %w", err)
	}

	result := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(result) {
			return nil, fmt.Errorf("embeddings: response index %d out of range", d.Index)
		}
		result[d.Index] = d.Embedding
	}
	return result, nil
}
