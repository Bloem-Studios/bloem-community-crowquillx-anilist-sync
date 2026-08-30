package device

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/crowquillx/silo-anilist-sync/anilist"
)

var fixedNow = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

func withNow(t *testing.T, at time.Time) {
	t.Helper()
	old := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = old })
}

func withBridge(t *testing.T, base string) {
	t.Helper()
	old := bridgeBaseURL
	bridgeBaseURL = base
	t.Cleanup(func() { bridgeBaseURL = old })
}

func withAnilistEndpoint(t *testing.T, endpoint string) {
	t.Helper()
	old := anilist.Endpoint
	anilist.Endpoint = endpoint
	t.Cleanup(func() { anilist.Endpoint = old })
}

func stateJSON(t *testing.T, code string, issuedAt time.Time) []byte {
	t.Helper()
	data, err := json.Marshal(providerState{Code: normalizeCode(code), IssuedAt: issuedAt.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func encryptEnvelope(t *testing.T, code, token string) envelope {
	t.Helper()
	key, err := deriveKey(normalizeCode(code))
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := aead.Seal(nil, nonce, []byte(token), []byte(aad))
	return envelope{
		V: 1,
		N: base64.RawURLEncoding.EncodeToString(nonce),
		C: base64.RawURLEncoding.EncodeToString(ciphertext),
	}
}

func TestStartProducesDeviceFlowFields(t *testing.T) {
	withNow(t, fixedNow)
	withBridge(t, "https://bridge.example")

	resp, err := NewService().Start(context.Background(), &pluginv1.WatchSyncDeviceAuthorizationServiceStartRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetFault() != nil {
		t.Fatalf("fault = %#v", resp.GetFault())
	}
	code := resp.GetUserCode()
	if !regexp.MustCompile(`^[0-9A-Z]{5}(-[0-9A-Z]{5}){3}$`).MatchString(code) {
		t.Fatalf("user_code = %q, want 4 dash-separated groups of 5", code)
	}
	for _, r := range strings.ReplaceAll(code, "-", "") {
		if !strings.ContainsRune(codeAlphabet, r) {
			t.Fatalf("user_code %q contains non-Crockford character %q", code, r)
		}
	}

	var state providerState
	if err := json.Unmarshal(resp.GetProviderState(), &state); err != nil {
		t.Fatalf("provider_state %q: %v", resp.GetProviderState(), err)
	}
	if state.Code != normalizeCode(code) || state.IssuedAt != fixedNow.Unix() {
		t.Fatalf("provider_state = %#v", state)
	}

	if got := resp.GetVerificationUrl(); got != "https://bridge.example/" {
		t.Fatalf("verification_url = %q", got)
	}
	if want := "https://bridge.example/connect?code=" + url.QueryEscape(code); resp.GetVerificationUrlComplete() != want {
		t.Fatalf("verification_url_complete = %q, want %q", resp.GetVerificationUrlComplete(), want)
	}
	if got := resp.GetPollingInterval().AsDuration(); got != 5*time.Second {
		t.Fatalf("polling_interval = %v, want 5s", got)
	}
	if want := fixedNow.Add(15 * time.Minute); !resp.GetExpiresAt().AsTime().Equal(want) {
		t.Fatalf("expires_at = %v, want %v", resp.GetExpiresAt().AsTime(), want)
	}
}

func TestPollPending(t *testing.T) {
	withNow(t, fixedNow)
	const code = "W8TWTC3Q0SDVJYWPYCEW"
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/poll/"+codeHash(code) {
			t.Errorf("poll path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	}))
	defer bridge.Close()
	withBridge(t, bridge.URL)

	resp, err := NewService().Poll(context.Background(), &pluginv1.WatchSyncDeviceAuthorizationServicePollRequest{
		ProviderState: stateJSON(t, code, fixedNow),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_PENDING {
		t.Fatalf("status = %v", resp.GetStatus())
	}
	if resp.GetFault() != nil {
		t.Fatalf("fault = %#v", resp.GetFault())
	}
	if resp.GetProviderState() != nil {
		t.Fatalf("provider_state = %q, want omitted", resp.GetProviderState())
	}
}

func TestPollAuthorized(t *testing.T) {
	withNow(t, fixedNow)
	const code = "W8TWTC3Q0SDVJYWPYCEW"
	const token = "eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjE3MDAwMDAwMDAsInN1YiI6IjEyMyJ9.e30"

	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/poll/"+codeHash(code) {
			t.Errorf("poll path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "authorized", "envelope": encryptEnvelope(t, code, token)})
	}))
	defer bridge.Close()
	withBridge(t, bridge.URL)

	viewer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Viewer": map[string]any{
			"id": 7, "name": "tester",
			"avatar":  map[string]any{"large": "http://x/y.png"},
			"siteUrl": "https://anilist.co/user/tester",
		}}})
	}))
	defer viewer.Close()
	withAnilistEndpoint(t, viewer.URL)

	resp, err := NewService().Poll(context.Background(), &pluginv1.WatchSyncDeviceAuthorizationServicePollRequest{
		ProviderState: stateJSON(t, code, fixedNow),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_AUTHORIZED {
		t.Fatalf("status = %v, fault = %#v", resp.GetStatus(), resp.GetFault())
	}
	if resp.GetFault() != nil {
		t.Fatalf("fault = %#v", resp.GetFault())
	}
	credentials := resp.GetCredentials()
	if credentials.GetAccessToken() != token || credentials.GetTokenType() != "Bearer" {
		t.Fatalf("credentials = %#v", credentials)
	}
	if credentials.GetSecretAttributes()["user_id"] != "7" {
		t.Fatalf("secret attributes = %#v", credentials.GetSecretAttributes())
	}
	if got := credentials.GetExpiresAt().AsTime(); !got.Equal(time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)) {
		t.Fatalf("expires_at = %v", got)
	}
	if resp.GetAccount().GetUsername() != "tester" || resp.GetAccount().GetExternalSubject() != "7" {
		t.Fatalf("account = %#v", resp.GetAccount())
	}
}

func TestPollBridgeExpired(t *testing.T) {
	withNow(t, fixedNow)
	const code = "W8TWTC3Q0SDVJYWPYCEW"
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "expired"})
	}))
	defer bridge.Close()
	withBridge(t, bridge.URL)

	resp, err := NewService().Poll(context.Background(), &pluginv1.WatchSyncDeviceAuthorizationServicePollRequest{
		ProviderState: stateJSON(t, code, fixedNow),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_EXPIRED {
		t.Fatalf("status = %v", resp.GetStatus())
	}
	if resp.GetFault() == nil || resp.GetFault().GetSafeMessage() == "" {
		t.Fatalf("fault = %#v", resp.GetFault())
	}
}

func TestPollTamperedEnvelope(t *testing.T) {
	withNow(t, fixedNow)
	const code = "W8TWTC3Q0SDVJYWPYCEW"
	const token = "token-value"

	env := encryptEnvelope(t, code, token)
	ciphertext, err := base64.RawURLEncoding.DecodeString(env.C)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[0] ^= 0xFF
	env.C = base64.RawURLEncoding.EncodeToString(ciphertext)

	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "authorized", "envelope": env})
	}))
	defer bridge.Close()
	withBridge(t, bridge.URL)

	var viewerCalls atomic.Int64
	viewer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		viewerCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Viewer": map[string]any{"id": 7, "name": "tester"}}})
	}))
	defer viewer.Close()
	withAnilistEndpoint(t, viewer.URL)

	resp, err := NewService().Poll(context.Background(), &pluginv1.WatchSyncDeviceAuthorizationServicePollRequest{
		ProviderState: stateJSON(t, code, fixedNow),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_EXPIRED {
		t.Fatalf("status = %v", resp.GetStatus())
	}
	if resp.GetFault() == nil || resp.GetFault().GetSafeMessage() != "stored authorization could not be read; start the connection again" {
		t.Fatalf("fault = %#v", resp.GetFault())
	}
	if viewerCalls.Load() != 0 {
		t.Fatalf("Viewer called %d times after tampered envelope", viewerCalls.Load())
	}
}

func TestPollExpiredWindowSkipsBridge(t *testing.T) {
	withNow(t, fixedNow)
	const code = "W8TWTC3Q0SDVJYWPYCEW"
	var bridgeCalls atomic.Int64
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		bridgeCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
	}))
	defer bridge.Close()
	withBridge(t, bridge.URL)

	resp, err := NewService().Poll(context.Background(), &pluginv1.WatchSyncDeviceAuthorizationServicePollRequest{
		ProviderState: stateJSON(t, code, fixedNow.Add(-16*time.Minute)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_EXPIRED {
		t.Fatalf("status = %v", resp.GetStatus())
	}
	if bridgeCalls.Load() != 0 {
		t.Fatalf("bridge called %d times, want 0", bridgeCalls.Load())
	}
}

func TestPollEmptyProviderState(t *testing.T) {
	withNow(t, fixedNow)
	resp, err := NewService().Poll(context.Background(), &pluginv1.WatchSyncDeviceAuthorizationServicePollRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_EXPIRED {
		t.Fatalf("status = %v", resp.GetStatus())
	}
	if resp.GetFault().GetCode() != pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_REQUEST {
		t.Fatalf("fault = %#v", resp.GetFault())
	}
}

func TestPollTransportErrorStaysPending(t *testing.T) {
	withNow(t, fixedNow)
	const code = "W8TWTC3Q0SDVJYWPYCEW"
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := closed.URL
	closed.Close()
	withBridge(t, url)

	resp, err := NewService().Poll(context.Background(), &pluginv1.WatchSyncDeviceAuthorizationServicePollRequest{
		ProviderState: stateJSON(t, code, fixedNow),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_PENDING {
		t.Fatalf("status = %v", resp.GetStatus())
	}
	if resp.GetFault() != nil {
		t.Fatalf("fault = %#v", resp.GetFault())
	}
	if resp.GetProviderState() != nil {
		t.Fatalf("provider_state = %q, want omitted", resp.GetProviderState())
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
			name:        "non anilist error keeps generic message but carries detail",
			err:         errors.New("network partition"),
			wantCode:    pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_TEMPORARY,
			wantMessage: "temporary AniList request failure (network partition)",
		},
		{
			name:        "transport failure carries underlying error text",
			err:         errors.New(`call AniList: Post "https://graphql.anilist.co": context deadline exceeded`),
			wantCode:    pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_TEMPORARY,
			wantMessage: `temporary AniList request failure (call AniList: Post "https://graphql.anilist.co": context deadline exceeded)`,
		},
		{
			name:        "long transport error is trimmed",
			err:         errors.New(strings.Repeat("x", 250)),
			wantCode:    pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_TEMPORARY,
			wantMessage: "temporary AniList request failure (" + strings.Repeat("x", 200) + "...)",
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
