package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

var Endpoint = "https://graphql.anilist.co"

const (
	defaultRequestsPerMinute = 30
	rateLimitWindow          = time.Minute
	rateLimitPadding         = 100 * time.Millisecond
)

var sharedRateLimiter = newRateLimiter(defaultRequestsPerMinute)

type Client struct {
	HTTPClient  *http.Client
	AccessToken string
	Endpoint    string
	limiter     *rateLimiter
}

type Error struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("AniList HTTP %d", e.Status)
}

type graphQLError struct {
	Message string `json:"message"`
}

type mediaResponse struct {
	Data struct {
		Media struct {
			Episodes       *int `json:"episodes"`
			MediaListEntry *struct {
				ID       int    `json:"id"`
				Progress int    `json:"progress"`
				Status   string `json:"status"`
			} `json:"mediaListEntry"`
		} `json:"Media"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type ListEntry struct {
	ID       int
	MediaID  int
	Status   string
	Progress int
	Media    struct {
		ID       int
		Format   string
		Episodes *int
		Title    struct {
			Romaji  string `json:"romaji"`
			English string `json:"english"`
			Native  string `json:"native"`
		} `json:"title"`
		StartDate struct {
			Year int `json:"year"`
		} `json:"startDate"`
	} `json:"media"`
}

func (e ListEntry) PreferredTitle() string {
	for _, title := range []string{e.Media.Title.English, e.Media.Title.Romaji, e.Media.Title.Native} {
		if title != "" {
			return title
		}
	}
	return ""
}

func (e ListEntry) CompletedProgress() int {
	progress := e.Progress
	if e.Status == "COMPLETED" && e.Media.Episodes != nil && *e.Media.Episodes > progress {
		progress = *e.Media.Episodes
	}
	if e.Status == "COMPLETED" && e.Media.Format == "MOVIE" && progress < 1 {
		progress = 1
	}
	return progress
}

func NewClient(token string, httpClient *http.Client) *Client {
	limiter := &rateLimiter{}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
		limiter = sharedRateLimiter
	}
	return &Client{HTTPClient: httpClient, AccessToken: token, Endpoint: Endpoint, limiter: limiter}
}

type rateLimiter struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func newRateLimiter(requestsPerMinute int) *rateLimiter {
	return &rateLimiter{interval: rateLimitInterval(requestsPerMinute)}
}

func rateLimitInterval(requestsPerMinute int) time.Duration {
	if requestsPerMinute < 1 {
		return 0
	}
	interval := rateLimitWindow / time.Duration(requestsPerMinute)
	return interval + max(rateLimitPadding, interval/20)
}

func (l *rateLimiter) wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	now := time.Now()
	ready := l.next
	if ready.Before(now) {
		ready = now
	}
	if l.interval > 0 {
		l.next = ready.Add(l.interval)
	}
	l.mu.Unlock()

	delay := time.Until(ready)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (l *rateLimiter) observe(header http.Header, status int) time.Duration {
	if l == nil {
		return 0
	}
	now := time.Now()
	interval := time.Duration(0)
	if limit, ok := nonNegativeHeaderInt(header, "X-RateLimit-Limit"); ok && limit > 0 {
		interval = rateLimitInterval(limit)
	}
	remaining, hasRemaining := nonNegativeHeaderInt(header, "X-RateLimit-Remaining")
	resetAfter, hasReset := rateLimitResetAfter(header, now)
	retryAfter := retryAfterDelay(header, now)
	if status == http.StatusTooManyRequests {
		if hasReset && resetAfter > retryAfter {
			retryAfter = resetAfter
		}
		if retryAfter <= 0 {
			retryAfter = rateLimitWindow
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if interval > 0 {
		l.interval = interval
	} else if status == http.StatusTooManyRequests && l.interval <= 0 {
		l.interval = rateLimitInterval(defaultRequestsPerMinute)
	}
	if l.interval > 0 {
		l.next = laterTime(l.next, now.Add(l.interval))
	}
	switch {
	case status == http.StatusTooManyRequests:
		l.next = laterTime(l.next, now.Add(retryAfter+rateLimitPadding))
	case hasRemaining && remaining <= 1 && hasReset:
		l.next = laterTime(l.next, now.Add(resetAfter+rateLimitPadding))
	case hasRemaining && remaining == 0:
		l.next = laterTime(l.next, now.Add(rateLimitWindow))
	case hasRemaining && hasReset && resetAfter > 0 && remaining > 1:
		required := resetAfter / time.Duration(remaining)
		if required > l.interval {
			l.interval = required
			l.next = laterTime(l.next, now.Add(required))
		}
	}
	return retryAfter
}

func nonNegativeHeaderInt(header http.Header, name string) (int, bool) {
	value, err := strconv.Atoi(header.Get(name))
	return value, err == nil && value >= 0
}

func retryAfterDelay(header http.Header, now time.Time) time.Duration {
	value := header.Get("Retry-After")
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt.Sub(now)
	}
	return 0
}

func rateLimitResetAfter(header http.Header, now time.Time) (time.Duration, bool) {
	resetAt, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		return 0, false
	}
	delay := time.Unix(resetAt, 0).Sub(now)
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

func laterTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func (c *Client) ListEntries(ctx context.Context, userID int) ([]ListEntry, error) {
	if userID < 1 {
		return nil, fmt.Errorf("AniList user ID is required")
	}
	var response struct {
		Data struct {
			MediaListCollection struct {
				Lists []struct {
					Entries []ListEntry `json:"entries"`
				} `json:"lists"`
			} `json:"MediaListCollection"`
		} `json:"data"`
	}
	query := `query ($userId: Int!) { MediaListCollection(userId: $userId, type: ANIME) { lists { entries { id mediaId status progress media { id format episodes title { romaji english native } startDate { year } } } } } }`
	if err := c.doWithLimit(ctx, query, map[string]any{"userId": userID}, &response, 64<<20); err != nil {
		return nil, err
	}
	entries := make([]ListEntry, 0)
	seen := make(map[int]bool)
	for _, list := range response.Data.MediaListCollection.Lists {
		for _, entry := range list.Entries {
			if seen[entry.ID] {
				continue
			}
			seen[entry.ID] = true
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].MediaID != entries[j].MediaID {
			return entries[i].MediaID < entries[j].MediaID
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

func (c *Client) AdvanceProgress(ctx context.Context, mediaID, progress int) error {
	if c.AccessToken == "" {
		return fmt.Errorf("AniList access token is not configured")
	}
	if progress < 1 {
		return nil
	}
	var current mediaResponse
	query := `query ($id: Int!) { Media(id: $id, type: ANIME) { episodes mediaListEntry { id progress status } } }`
	if err := c.do(ctx, query, map[string]any{"id": mediaID}, &current); err != nil {
		return err
	}
	episodes := current.Data.Media.Episodes
	if episodes != nil && *episodes > 0 && progress > *episodes {
		return fmt.Errorf("mapped progress %d exceeds AniList episode count %d", progress, *episodes)
	}
	entry := current.Data.Media.MediaListEntry
	if entry == nil {
		status := "CURRENT"
		if episodes != nil && progress == *episodes {
			status = "COMPLETED"
		}
		mutation := `mutation ($mediaId: Int!, $progress: Int!, $status: MediaListStatus!) { SaveMediaListEntry(mediaId: $mediaId, progress: $progress, status: $status) { id } }`
		return c.do(ctx, mutation, map[string]any{"mediaId": mediaID, "progress": progress, "status": status}, &struct{}{})
	}
	if entry.Status == "COMPLETED" || entry.Progress >= progress {
		return nil
	}
	status := entry.Status
	switch entry.Status {
	case "PLANNING":
		status = "CURRENT"
	case "CURRENT":
		if episodes != nil && progress == *episodes {
			status = "COMPLETED"
		}
	}
	if status == entry.Status {
		mutation := `mutation ($id: Int!, $progress: Int!) { SaveMediaListEntry(id: $id, progress: $progress) { id } }`
		return c.do(ctx, mutation, map[string]any{"id": entry.ID, "progress": progress}, &struct{}{})
	}
	mutation := `mutation ($id: Int!, $progress: Int!, $status: MediaListStatus!) { SaveMediaListEntry(id: $id, progress: $progress, status: $status) { id } }`
	return c.do(ctx, mutation, map[string]any{"id": entry.ID, "progress": progress, "status": status}, &struct{}{})
}

func (c *Client) do(ctx context.Context, query string, variables map[string]any, out any) error {
	return c.doWithLimit(ctx, query, variables, out, 4<<20)
}

func (c *Client) doWithLimit(ctx context.Context, query string, variables map[string]any, out any, maxBytes int64) error {
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
	if err := c.limiter.wait(ctx); err != nil {
		return fmt.Errorf("wait for AniList request allowance: %w", err)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("call AniList: %w", err)
	}
	defer resp.Body.Close()
	retryAfter := c.limiter.observe(resp.Header, resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return &Error{Status: resp.StatusCode, RetryAfter: retryAfter}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return fmt.Errorf("read AniList response: %w", err)
	}
	var envelope struct {
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode AniList response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return &Error{Status: resp.StatusCode, Message: "AniList GraphQL: " + envelope.Errors[0].Message}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode AniList response data: %w", err)
	}
	return nil
}
