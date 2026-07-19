package mapping

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

	dataset, err := c.download(ctx)
	if err != nil {
		if c.dataset != nil {
			return c.dataset, nil
		}
		return nil, err
	}
	c.dataset = dataset
	c.loadedAt = time.Now()
	return dataset, nil
}

func (c *Client) download(ctx context.Context) (Dataset, error) {
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
	const maxMappingsSize = 64 << 20
	limited := io.LimitReader(resp.Body, maxMappingsSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read AniBridge mappings: %w", err)
	}
	if len(data) > maxMappingsSize {
		return nil, fmt.Errorf("AniBridge mappings exceed %d bytes", maxMappingsSize)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode AniBridge mappings: %w", err)
	}
	var metadata struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(raw["$meta"], &metadata); err != nil || !strings.HasPrefix(metadata.SchemaVersion, "3.") {
		return nil, fmt.Errorf("unsupported AniBridge schema version %q", metadata.SchemaVersion)
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
	return dataset, nil
}
