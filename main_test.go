package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	"github.com/crowquillx/silo-anilist-sync/anilist"
	"github.com/crowquillx/silo-anilist-sync/mapping"
)

func TestExchangeAPIKeyConnectsAndSetsExpiry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Viewer": map[string]any{
			"id": 7, "name": "tester",
			"avatar":  map[string]any{"large": "http://x/y.png"},
			"siteUrl": "https://anilist.co/user/tester",
		}}})
	}))
	defer upstream.Close()
	old := anilist.Endpoint
	anilist.Endpoint = upstream.URL
	t.Cleanup(func() { anilist.Endpoint = old })

	resp, err := (&server{}).ExchangeAPIKey(context.Background(), &pluginv1.WatchSyncExchangeAPIKeyRequest{
		ApiKey: "eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjE3MDAwMDAwMDAsInN1YiI6IjEyMyJ9.e30",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetFault() != nil {
		t.Fatalf("fault = %#v", resp.GetFault())
	}
	credentials := resp.GetCredentials()
	if credentials.GetAccessToken() == "" || credentials.GetTokenType() != "Bearer" {
		t.Fatalf("credentials = %#v", credentials)
	}
	if resp.GetAccount().GetUsername() != "tester" {
		t.Fatalf("account = %#v", resp.GetAccount())
	}
	if got := credentials.GetExpiresAt().AsTime(); !got.Equal(time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)) {
		t.Fatalf("expires_at = %v, want 2023-11-14T22:13:20Z", got)
	}
	if credentials.GetSecretAttributes()["user_id"] != "7" {
		t.Fatalf("secret attributes = %#v", credentials.GetSecretAttributes())
	}
}

func TestExchangeAPIKeyRejectsInvalidCredential(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()
	old := anilist.Endpoint
	anilist.Endpoint = upstream.URL
	t.Cleanup(func() { anilist.Endpoint = old })

	resp, err := (&server{}).ExchangeAPIKey(context.Background(), &pluginv1.WatchSyncExchangeAPIKeyRequest{
		ApiKey: "garbage.token.value",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetFault().GetCode() != pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_CREDENTIAL {
		t.Fatalf("fault = %#v", resp.GetFault())
	}
}

func TestResolveTargetsRequiresProviderConvergence(t *testing.T) {
	dataset := mapping.Catalog{AniBridge: mapping.Dataset{
		"tvdb_show:1:s1": {"anilist:10": {"1-12": "1-12"}},
		"tmdb_show:2:s1": {"anilist:11": {"1-12": "1-12"}},
	}}
	_, err := resolveTargets(context.Background(), dataset, &pluginv1.WatchSyncMedia{
		MediaType:         pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_EPISODE,
		SeasonNumber:      1,
		EpisodeNumber:     3,
		SeriesExternalIds: map[string]string{"tvdb": "1", "tmdb": "2"},
	})
	if err == nil {
		t.Fatal("expected disagreeing mappings to fail")
	}
}

func TestResolveTargetsMapsEpisode(t *testing.T) {
	dataset := mapping.Catalog{AniBridge: mapping.Dataset{
		"tvdb_show:1:s1": {"anilist:10": {"1-12": "1-12"}},
		"tmdb_show:2:s1": {"anilist:10": {"1-12": "1-12"}},
	}}
	targets, err := resolveTargets(context.Background(), dataset, &pluginv1.WatchSyncMedia{
		MediaType:         pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_EPISODE,
		SeasonNumber:      1,
		EpisodeNumber:     3,
		SeriesExternalIds: map[string]string{"tvdb": "1", "tmdb": "2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if targets[10] != 3 {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestApplyEventRejectsUnwatch(t *testing.T) {
	result := applyEvent(context.Background(), nil, mapping.Catalog{}, &pluginv1.WatchSyncEvent{
		EventId:   "event-1",
		Operation: pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_MARK_UNWATCHED,
	}, watchBehavior{})
	if result.GetStatus() != pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_REJECTED {
		t.Fatalf("status = %v", result.GetStatus())
	}
}

func TestMappingServiceFailureRetries(t *testing.T) {
	result := applyErrorResult("event-1", &mapping.TemporaryError{Err: errors.New("ARM offline")})
	if result.GetStatus() != pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_RETRY {
		t.Fatalf("status = %v", result.GetStatus())
	}
	if result.GetFault().GetSafeMessage() != "temporary anime mapping service failure" {
		t.Fatalf("fault = %#v", result.GetFault())
	}
}

func TestManifestAdvertisesWatchedImportAndDeviceCodeConfiguration(t *testing.T) {
	manifest, err := publicmanifest.Load(manifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.GetCapabilities()) != 1 {
		t.Fatalf("capabilities = %#v", manifest.GetCapabilities())
	}
	descriptor := manifest.GetCapabilities()[0].GetWatchSyncProvider()
	if !descriptor.GetImportWatched() || !descriptor.GetScrobblePlayback() {
		t.Fatalf("watch sync descriptor = %#v", descriptor)
	}
	authMethods := descriptor.GetAuthMethods()
	if len(authMethods) != 1 || authMethods[0] != pluginv1.WatchSyncAuthMethod_WATCH_SYNC_AUTH_METHOD_DEVICE_CODE {
		t.Fatalf("auth methods = %#v", authMethods)
	}
	if len(manifest.GetGlobalConfigSchema()) != 1 {
		t.Fatalf("global config schema = %#v", manifest.GetGlobalConfigSchema())
	}
	schema := manifest.GetGlobalConfigSchema()[0]
	var jsonSchema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal([]byte(schema.GetJsonSchema()), &jsonSchema); err != nil {
		t.Fatal(err)
	}
	if len(jsonSchema.Properties) != 2 {
		t.Fatalf("schema properties = %#v", jsonSchema.Properties)
	}
	if _, ok := jsonSchema.Properties["client_id"]; ok {
		t.Fatal("schema still advertises client_id")
	}
	if _, ok := jsonSchema.Properties["client_secret"]; ok {
		t.Fatal("schema still advertises client_secret")
	}
	if len(jsonSchema.Required) != 0 {
		t.Fatalf("schema required = %#v", jsonSchema.Required)
	}
	fields := schema.GetAdminForm().GetFields()
	if len(fields) != 2 || fields[0].GetKey() != "sync_manual_watched" ||
		fields[1].GetKey() != "playback_completion_percent" {
		t.Fatalf("provider fields = %#v", fields)
	}
}

func TestRemoteStatesExpandAniListProgressIntoMappedEpisodes(t *testing.T) {
	entry := anilist.ListEntry{ID: 9, MediaID: 42, Status: "CURRENT", Progress: 2}
	entry.Media.ID = 42
	entry.Media.Format = "TV"
	entry.Media.Title.English = "Example Anime"
	entry.Media.StartDate.Year = 2024
	states, err := remoteStates([]anilist.ListEntry{entry}, mapping.Catalog{AniBridge: mapping.Dataset{
		"tvdb_show:100:s2": {"anilist:42": {"1-12": "1-12"}},
		"tmdb_show:200:s2": {"anilist:42": {"1-12": "1-12"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("states = %#v", states)
	}
	second := states[1]
	if second.GetProviderItemKey() != "anilist:9:s2:e2" ||
		second.GetMedia().GetMediaType() != pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_EPISODE ||
		second.GetMedia().GetSeriesExternalIds()["anilist"] != "42" ||
		second.GetMedia().GetSeriesExternalIds()["tvdb"] != "100" ||
		second.GetMedia().GetSeriesExternalIds()["tmdb"] != "200" ||
		second.GetWatched().GetPlayCount() != 1 {
		t.Fatalf("second state = %#v", second)
	}
}

func TestRemotePageTokenRejectsMalformedValues(t *testing.T) {
	if _, _, err := remotePageToken("1:2:3"); err == nil {
		t.Fatal("expected malformed token to fail")
	}
	if page, offset, err := remotePageToken("2:7"); err != nil || page != 2 || offset != 7 {
		t.Fatalf("remotePageToken() = %d, %d, %v", page, offset, err)
	}
}

func TestManualWatchedMarksRequireExplicitToggle(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Media": map[string]any{
				"episodes": 12, "mediaListEntry": nil,
			}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"SaveMediaListEntry": map[string]any{"id": 1}}})
	}))
	defer upstream.Close()
	client := anilist.NewClient("token", upstream.Client())
	client.Endpoint = upstream.URL
	dataset := mapping.Catalog{AniBridge: mapping.Dataset{
		"tvdb_show:1:s1": {"anilist:10": {"1-12": "1-12"}},
	}}
	event := &pluginv1.WatchSyncEvent{
		EventId:   "manual-1",
		Operation: pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_MARK_WATCHED,
		Origin:    pluginv1.WatchSyncOrigin_WATCH_SYNC_ORIGIN_RECONCILIATION,
		Media: &pluginv1.WatchSyncMedia{
			MediaType:         pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_EPISODE,
			SeasonNumber:      1,
			EpisodeNumber:     3,
			SeriesExternalIds: map[string]string{"tvdb": "1"},
		},
	}
	if result := applyEvent(context.Background(), client, dataset, event, watchBehavior{}); result.GetStatus() != pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_NO_CHANGE {
		t.Fatalf("disabled status = %v", result.GetStatus())
	}
	if calls != 0 {
		t.Fatalf("disabled manual sync made %d AniList calls", calls)
	}
	if result := applyEvent(context.Background(), client, dataset, event, watchBehavior{syncManualWatched: true}); result.GetStatus() != pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_APPLIED {
		t.Fatalf("enabled status = %v, fault = %#v", result.GetStatus(), result.GetFault())
	}
	if calls != 2 {
		t.Fatalf("enabled manual sync made %d AniList calls, want 2", calls)
	}
}

func TestPlaybackStopUsesConfiguredCompletionThreshold(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Media": map[string]any{
				"episodes": 12, "mediaListEntry": nil,
			}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"SaveMediaListEntry": map[string]any{"id": 1}}})
	}))
	defer upstream.Close()
	client := anilist.NewClient("token", upstream.Client())
	client.Endpoint = upstream.URL
	dataset := mapping.Catalog{AniBridge: mapping.Dataset{
		"tvdb_show:1:s1": {"anilist:10": {"1-12": "1-12"}},
	}}
	event := &pluginv1.WatchSyncEvent{
		EventId:           "playback-1",
		Operation:         pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_SCROBBLE_STOP,
		CompletionPercent: 90,
		Media: &pluginv1.WatchSyncMedia{
			MediaType:         pluginv1.WatchSyncMediaType_WATCH_SYNC_MEDIA_TYPE_EPISODE,
			SeasonNumber:      1,
			EpisodeNumber:     3,
			SeriesExternalIds: map[string]string{"tvdb": "1"},
		},
	}
	behavior := watchBehaviorFromConfig(&pluginv1.WatchSyncProviderConfig{Values: map[string]string{
		"provider.playback_completion_percent": "90",
	}})
	if result := applyEvent(context.Background(), client, dataset, event, behavior); result.GetStatus() != pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_NO_CHANGE {
		t.Fatalf("threshold status = %v", result.GetStatus())
	}
	event.CompletionPercent = 90.1
	if result := applyEvent(context.Background(), client, dataset, event, behavior); result.GetStatus() != pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_APPLIED {
		t.Fatalf("completed status = %v, fault = %#v", result.GetStatus(), result.GetFault())
	}
	if calls != 2 {
		t.Fatalf("completed playback made %d AniList calls, want 2", calls)
	}
}

func TestIgnoredManualEventsDoNotLoadMappings(t *testing.T) {
	response, err := (&server{}).ApplyEvents(context.Background(), &pluginv1.WatchSyncApplyEventsRequest{
		Context: &pluginv1.WatchSyncAuthenticatedContext{
			ProviderConfig: &pluginv1.WatchSyncProviderConfig{},
		},
		Events: []*pluginv1.WatchSyncEvent{{
			EventId:   "manual-1",
			Operation: pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_MARK_WATCHED,
			Origin:    pluginv1.WatchSyncOrigin_WATCH_SYNC_ORIGIN_RECONCILIATION,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetFault() != nil || len(response.GetResults()) != 1 ||
		response.GetResults()[0].GetStatus() != pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_NO_CHANGE {
		t.Fatalf("response = %#v", response)
	}
}

func TestFaultFromError(t *testing.T) {
	longPending := 90 * time.Second
	tests := []struct {
		name        string
		err         error
		wantCode    pluginv1.WatchSyncFaultCode
		wantMessage string
		wantRetry   time.Duration
	}{
		{
			name:        "graphql too many requests is rate limited",
			err:         &anilist.Error{Status: http.StatusOK, Message: "AniList GraphQL: Too Many Requests"},
			wantCode:    pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_RATE_LIMITED,
			wantMessage: "AniList rate limit reached",
			wantRetry:   60 * time.Second,
		},
		{
			name:        "graphql too many requests keeps retry after",
			err:         &anilist.Error{Status: http.StatusOK, Message: "AniList GraphQL: Too Many Requests", RetryAfter: longPending},
			wantCode:    pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_RATE_LIMITED,
			wantMessage: "AniList rate limit reached",
			wantRetry:   longPending,
		},
		{
			name:        "http 500 is temporary with detail",
			err:         &anilist.Error{Status: http.StatusInternalServerError},
			wantCode:    pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_TEMPORARY,
			wantMessage: "temporary AniList request failure (HTTP 500)",
		},
		{
			name:        "graphql validation failure is temporary with detail",
			err:         &anilist.Error{Status: http.StatusOK, Message: "AniList GraphQL: Validation Failed (f.name)."},
			wantCode:    pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_TEMPORARY,
			wantMessage: "temporary AniList request failure (AniList GraphQL: Validation Failed (f.name).)",
		},
		{
			name:        "http 404 is permanent with detail",
			err:         &anilist.Error{Status: http.StatusNotFound},
			wantCode:    pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_PERMANENT,
			wantMessage: "AniList rejected the request (HTTP 404)",
		},
		{
			name:        "non anilist error keeps generic temporary message",
			err:         errors.New("network partition"),
			wantCode:    pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_TEMPORARY,
			wantMessage: "temporary AniList request failure",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fault := faultFromError(tt.err)
			if fault.GetCode() != tt.wantCode {
				t.Fatalf("code = %v, want %v", fault.GetCode(), tt.wantCode)
			}
			if fault.GetSafeMessage() != tt.wantMessage {
				t.Fatalf("safe_message = %q, want %q", fault.GetSafeMessage(), tt.wantMessage)
			}
			if got := fault.GetRetryAfter().AsDuration(); got != tt.wantRetry {
				t.Fatalf("retry_after = %v, want %v", got, tt.wantRetry)
			}
		})
	}
}

// stubMappingTransport serves canned AniBridge and Anime-Lists artifacts for
// the mapping.Client without touching the network, mirroring how tests stub
// anilist.Endpoint.
type stubMappingTransport struct{}

func (stubMappingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	status, body := http.StatusNotFound, ""
	switch req.URL.String() {
	case mapping.DefaultURL:
		status, body = http.StatusOK, `{"$meta":{"schema_version":"3.0.0"},"tvdb_show:100:s1":{"anilist:10":{"1-12":"1-12"}},"tvdb_show:200:s1":{"anilist:11":{"1-12":"1-12"}}}`
	case mapping.DefaultAnimeListsURL:
		status, body = http.StatusOK, `<anime-list/>`
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func TestListRemoteStateWalksPaginationWithSingleUpstreamImport(t *testing.T) {
	anilistCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anilistCalls++
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Variables["userId"] != float64(7) {
			t.Fatalf("variables = %#v", body.Variables)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"MediaListCollection": map[string]any{
			"lists": []map[string]any{{"entries": []map[string]any{
				{
					"id": 1, "mediaId": 10, "status": "CURRENT", "progress": 1,
					"media": map[string]any{
						"id": 10, "format": "TV", "episodes": 12,
						"title":     map[string]any{"romaji": "Alpha"},
						"startDate": map[string]any{"year": 2020},
					},
				},
				{
					"id": 2, "mediaId": 11, "status": "CURRENT", "progress": 1,
					"media": map[string]any{
						"id": 11, "format": "TV", "episodes": 12,
						"title":     map[string]any{"romaji": "Beta"},
						"startDate": map[string]any{"year": 2021},
					},
				},
			}}},
		}}})
	}))
	defer upstream.Close()
	old := anilist.Endpoint
	anilist.Endpoint = upstream.URL
	t.Cleanup(func() { anilist.Endpoint = old })

	srv := &server{mappings: mapping.NewClient(&http.Client{Transport: stubMappingTransport{}})}
	request := func(pageToken string) *pluginv1.WatchSyncListRemoteStateRequest {
		return &pluginv1.WatchSyncListRemoteStateRequest{
			PageToken: pageToken,
			PageSize:  1,
			Context: &pluginv1.WatchSyncAuthenticatedContext{
				Credentials: &pluginv1.WatchSyncCredentials{
					AccessToken:      "token",
					SecretAttributes: map[string]string{"user_id": "7"},
				},
			},
		}
	}

	total := 0
	pageToken := ""
	for pages := 0; ; pages++ {
		response, err := srv.ListRemoteState(context.Background(), request(pageToken))
		if err != nil {
			t.Fatal(err)
		}
		if response.GetFault() != nil {
			t.Fatalf("fault = %#v", response.GetFault())
		}
		total += len(response.GetItems())
		if pages >= 10 {
			t.Fatal("pagination walk did not terminate")
		}
		if response.GetNextPageToken() == "" {
			if response.GetNextCursor() != "full-v1" {
				t.Fatalf("next_cursor = %q, want full-v1", response.GetNextCursor())
			}
			break
		}
		pageToken = response.GetNextPageToken()
	}
	if total != 2 {
		t.Fatalf("total items = %d, want 2", total)
	}
	if anilistCalls != 1 {
		t.Fatalf("AniList upstream calls = %d, want 1 cached import across pages", anilistCalls)
	}
}
