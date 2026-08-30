package device

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// bridgeBaseURL is injectable for tests; it defaults to the production bridge.
var bridgeBaseURL = "https://anilist.crowquill.dev"

var bridgeHTTPClient = &http.Client{Timeout: 10 * time.Second}

type bridgePollResponse struct {
	Status   string    `json:"status"`
	Envelope *envelope `json:"envelope,omitempty"`
}

func pollBridge(ctx context.Context, hash string) (*bridgePollResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bridgeBaseURL+"/api/poll/"+hash, nil)
	if err != nil {
		return nil, fmt.Errorf("build bridge poll request: %w", err)
	}
	resp, err := bridgeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll bridge: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bridge poll returned HTTP %d", resp.StatusCode)
	}
	var out bridgePollResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode bridge poll response: %w", err)
	}
	return &out, nil
}
