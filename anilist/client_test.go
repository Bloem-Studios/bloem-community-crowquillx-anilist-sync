package anilist

import (
	"context"
	"encoding/json"

	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestAdvanceProgressDoesNotDecrease(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"Media": map[string]any{
				"episodes":       12,
				"mediaListEntry": map[string]any{"id": 3, "progress": 8, "status": "CURRENT"},
			}},
		})
	}))
	defer server.Close()
	client := NewClient("token", server.Client())
	client.Endpoint = server.URL
	if err := client.AdvanceProgress(context.Background(), 1, 6); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestAdvanceProgressCompletesNewEntryAtEpisodeCount(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"Media": map[string]any{"episodes": 12, "mediaListEntry": nil}},
			})
			return
		}
		if body.Variables["progress"] != float64(12) || body.Variables["status"] != "COMPLETED" || body.Variables["mediaId"] != float64(1) {
			t.Fatalf("mutation variables = %#v", body.Variables)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"SaveMediaListEntry": map[string]any{"id": 1}}})
	}))
	defer server.Close()
	client := NewClient("token", server.Client())
	client.Endpoint = server.URL
	if err := client.AdvanceProgress(context.Background(), 1, 12); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestAdvanceProgressPreservesPausedStatusAndUsesEntryID(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Media": map[string]any{
				"episodes": 12, "mediaListEntry": map[string]any{"id": 77, "progress": 3, "status": "PAUSED"},
			}}})
			return
		}
		if body.Variables["id"] != float64(77) || body.Variables["progress"] != float64(4) {
			t.Fatalf("mutation variables = %#v", body.Variables)
		}
		if _, ok := body.Variables["status"]; ok {
			t.Fatalf("paused status should be preserved: %#v", body.Variables)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"SaveMediaListEntry": map[string]any{"id": 77}}})
	}))
	defer server.Close()
	client := NewClient("token", server.Client())
	client.Endpoint = server.URL
	if err := client.AdvanceProgress(context.Background(), 1, 4); err != nil {
		t.Fatal(err)
	}
}

func TestAdvanceProgressRejectsMappingPastKnownTotal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Media": map[string]any{
			"episodes": 12, "mediaListEntry": nil,
		}}})
	}))
	defer server.Close()
	client := NewClient("token", server.Client())
	client.Endpoint = server.URL
	if err := client.AdvanceProgress(context.Background(), 1, 13); err == nil {
		t.Fatal("expected oversized mapped progress to fail")
	}
}

func TestListEntriesFlattensListsAndDeduplicatesEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Variables["userId"] != float64(7) {
			t.Fatalf("variables = %#v", body.Variables)
		}
		if body.Query == "" {
			t.Fatal("query is empty")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"MediaListCollection": map[string]any{
			"lists": []map[string]any{
				{"entries": []map[string]any{{
					"id": 9, "mediaId": 42, "status": "COMPLETED", "progress": 0,
					"media": map[string]any{
						"id": 42, "format": "TV", "episodes": 12,
						"title":     map[string]any{"romaji": "Romaji", "english": "English"},
						"startDate": map[string]any{"year": 2024},
					},
				}}},
				{"entries": []map[string]any{
					{
						"id": 10, "mediaId": 7, "status": "CURRENT", "progress": 3,
						"media": map[string]any{
							"id": 7, "format": "TV", "episodes": 24,
							"title":     map[string]any{"romaji": "Early"},
							"startDate": map[string]any{"year": 2023},
						},
					},
					{
						"id": 9, "mediaId": 42, "status": "COMPLETED", "progress": 0,
						"media": map[string]any{
							"id": 42, "format": "TV", "episodes": 12,
							"title":     map[string]any{"romaji": "Romaji", "english": "English"},
							"startDate": map[string]any{"year": 2024},
						},
					},
				}},
			},
		}}})
	}))
	defer server.Close()
	client := NewClient("token", server.Client())
	client.Endpoint = server.URL
	entries, err := client.ListEntries(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %#v, want 2 after deduplicating entry 9", entries)
	}
	if entries[0].MediaID != 7 || entries[0].ID != 10 {
		t.Fatalf("entries[0] = %#v, want mediaId 7 entry 10 first", entries[0])
	}
	entry := entries[1]
	if entry.MediaID != 42 || entry.ID != 9 || entry.Media.ID != 42 || entry.PreferredTitle() != "English" || entry.CompletedProgress() != 12 {
		t.Fatalf("entries[1] = %#v", entry)
	}
}

func TestClientPacesRequestsFromRateLimitHeaders(t *testing.T) {
	var lastRequest time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		now := time.Now()
		if !lastRequest.IsZero() && now.Sub(lastRequest) < 75*time.Millisecond {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		lastRequest = now
		w.Header().Set("X-RateLimit-Limit", "600")
		w.Header().Set("X-RateLimit-Remaining", "599")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"MediaListCollection": map[string]any{
			"lists": []any{},
		}}})
	}))
	defer server.Close()

	client := NewClient("token", server.Client())
	client.Endpoint = server.URL
	for request := 1; request <= 2; request++ {
		if _, err := client.ListEntries(context.Background(), 7); err != nil {
			t.Fatalf("ListEntries(%d): %v", request, err)
		}
	}
}

func TestClientPreservesRateLimitRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient("token", server.Client())
	client.Endpoint = server.URL
	_, err := client.ListEntries(context.Background(), 7)
	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error = %T %v, want *Error", err, err)
	}
	if apiErr.Status != http.StatusTooManyRequests || apiErr.RetryAfter != time.Minute {
		t.Fatalf("error = %#v, want HTTP 429 with 1m retry", apiErr)
	}
}

func TestListEntriesLongBudgetAllowsSlowLargeListImport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"MediaListCollection": map[string]any{
			"lists": []any{},
		}}})
	}))
	defer server.Close()

	// The default 15s budget must be extended for full-list imports.
	client := NewClient("token", nil)
	client.Endpoint = server.URL
	if _, err := client.ListEntries(context.Background(), 7); err != nil {
		t.Fatalf("default-budget client failed on a slow import: %v", err)
	}

	// An explicit caller-configured timeout must still be honored.
	short := NewClient("token", &http.Client{Timeout: time.Second})
	short.Endpoint = server.URL
	started := time.Now()
	if _, err := short.ListEntries(context.Background(), 7); err == nil {
		t.Fatal("explicit 1s timeout should still fail on a 3s import")
	}
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond {
		t.Fatalf("short client failed after %v, want the 1s timeout to fire", elapsed)
	}
}

func TestLimiterStretchesIntervalAcrossRemainingBudget(t *testing.T) {
	limiter := newRateLimiter(defaultRequestsPerMinute)
	resetAt := time.Now().Add(120 * time.Second)
	header := http.Header{}
	header.Set("X-RateLimit-Limit", "30")
	header.Set("X-RateLimit-Remaining", "2")
	header.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))
	limiter.observe(header, http.StatusOK)

	limiter.mu.Lock()
	next := limiter.next
	interval := limiter.interval
	limiter.mu.Unlock()

	// The window has ~120s left and 2 requests of budget, so the limiter must
	// space them ~60s apart instead of the 2s derived from the 30/min limit.
	delay := time.Until(next)
	if delay < 55*time.Second || delay > 65*time.Second {
		t.Fatalf("delay = %v, want roughly 60s (120s reset horizon / 2 remaining)", delay)
	}
	if interval < 55*time.Second || interval > 65*time.Second {
		t.Fatalf("interval = %v, want roughly 60s", interval)
	}
}
