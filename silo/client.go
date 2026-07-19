package silo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	APIKey     string
}

type Item struct {
	ContentID     string `json:"content_id"`
	Type          string `json:"type"`
	SeriesID      string `json:"series_id"`
	SeasonNumber  *int   `json:"season_number"`
	EpisodeNumber *int   `json:"episode_number"`
	EpisodeCount  *int   `json:"episode_count"`
	ImdbID        string `json:"imdb_id"`
	TmdbID        string `json:"tmdb_id"`
	TvdbID        string `json:"tvdb_id"`
}

type Episode struct {
	ContentID     string `json:"content_id"`
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
}

type episodesResponse struct {
	Episodes []Episode `json:"episodes"`
}

func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{HTTPClient: httpClient, BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey}
}

func (c *Client) GetItem(ctx context.Context, profileID, contentID string) (Item, error) {
	var item Item
	err := c.get(ctx, profileID, "/api/v1/catalog/items/"+url.PathEscape(contentID), &item)
	return item, err
}

func (c *Client) GetEpisodes(ctx context.Context, profileID, seriesID string) ([]Episode, error) {
	var response episodesResponse
	if err := c.get(ctx, profileID, "/api/v1/catalog/items/"+url.PathEscape(seriesID)+"/episodes", &response); err != nil {
		return nil, err
	}
	return response.Episodes, nil
}

func (c *Client) get(ctx context.Context, profileID, path string, out any) error {
	if c.BaseURL == "" {
		return fmt.Errorf("Silo base URL is not configured or discoverable")
	}
	if c.APIKey == "" {
		return fmt.Errorf("Silo API key is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create Silo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if profileID != "" {
		req.Header.Set("X-Profile-Id", profileID)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request Silo item metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request Silo item metadata: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode Silo item metadata: %w", err)
	}
	return nil
}
