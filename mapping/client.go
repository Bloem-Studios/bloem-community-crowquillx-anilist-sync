package mapping

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const DefaultURL = "https://github.com/anibridge/anibridge-mappings/releases/download/v3/mappings.min.json"

type Client struct {
	url        string
	httpClient *http.Client
	maxAge     time.Duration

	mu       sync.RWMutex
	dataset  Dataset
	loadedAt time.Time
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{url: DefaultURL, httpClient: httpClient, maxAge: 24 * time.Hour}
}

func (c *Client) Dataset(ctx context.Context) (Dataset, error) {
	c.mu.RLock()
	if c.dataset != nil && time.Since(c.loadedAt) < c.maxAge {
		dataset := c.dataset
		c.mu.RUnlock()
		return dataset, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dataset != nil && time.Since(c.loadedAt) < c.maxAge {
		return c.dataset, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("create mappings request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download AniBridge mappings: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download AniBridge mappings: HTTP %d", resp.StatusCode)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode AniBridge mappings: %w", err)
	}
	dataset := make(Dataset, len(raw)-1)
	for descriptor, encoded := range raw {
		if descriptor == "$meta" {
			continue
		}
		var targets map[string]map[string]string
		if err := json.Unmarshal(encoded, &targets); err != nil {
			return nil, fmt.Errorf("decode AniBridge descriptor %q: %w", descriptor, err)
		}
		dataset[descriptor] = targets
	}
	c.dataset = dataset
	c.loadedAt = time.Now()
	return dataset, nil
}
