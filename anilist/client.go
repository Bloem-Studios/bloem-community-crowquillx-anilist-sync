package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const endpoint = "https://graphql.anilist.co"

type Client struct {
	HTTPClient  *http.Client
	AccessToken string
	Endpoint    string
}

type graphQLError struct {
	Message string `json:"message"`
}

type mediaResponse struct {
	Data struct {
		Media struct {
			Episodes       *int `json:"episodes"`
			MediaListEntry *struct {
				Progress int    `json:"progress"`
				Status   string `json:"status"`
			} `json:"mediaListEntry"`
		} `json:"Media"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

func NewClient(token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{HTTPClient: httpClient, AccessToken: token, Endpoint: endpoint}
}

func (c *Client) AdvanceProgress(ctx context.Context, mediaID, progress int) error {
	if c.AccessToken == "" {
		return fmt.Errorf("AniList access token is not configured")
	}
	if progress < 1 {
		return nil
	}
	var current mediaResponse
	query := `query ($id: Int!) { Media(id: $id, type: ANIME) { episodes mediaListEntry { progress status } } }`
	if err := c.do(ctx, query, map[string]any{"id": mediaID}, &current); err != nil {
		return err
	}
	entry := current.Data.Media.MediaListEntry
	if entry != nil && (entry.Status == "COMPLETED" || entry.Progress >= progress) {
		return nil
	}
	status := "CURRENT"
	if current.Data.Media.Episodes != nil && *current.Data.Media.Episodes > 0 && progress >= *current.Data.Media.Episodes {
		progress = *current.Data.Media.Episodes
		status = "COMPLETED"
	}
	mutation := `mutation ($id: Int!, $progress: Int!, $status: MediaListStatus!) { SaveMediaListEntry(mediaId: $id, progress: $progress, status: $status) { id } }`
	var result struct {
		Errors []graphQLError `json:"errors"`
	}
	return c.do(ctx, mutation, map[string]any{"id": mediaID, "progress": progress, "status": status}, &result)
}

func (c *Client) do(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return fmt.Errorf("encode AniList request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create AniList request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("call AniList: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("call AniList: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode AniList response: %w", err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	var envelope struct {
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.Errors) > 0 {
		return fmt.Errorf("AniList GraphQL: %s", envelope.Errors[0].Message)
	}
	return nil
}
