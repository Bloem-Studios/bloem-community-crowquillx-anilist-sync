package device

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/crowquillx/silo-anilist-sync/anilist"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	codeAlphabet  = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	codeLength    = 20
	codeGroupSize = 5

	codeValidity    = 15 * time.Minute
	pollingInterval = 5 * time.Second
)

// now is injectable for expiry tests; it defaults to time.Now.
var now = time.Now

type Service struct {
	pluginv1.UnimplementedWatchSyncDeviceAuthorizationServiceServer
}

func NewService() *Service {
	return &Service{}
}

// providerState is the opaque flow state handed back to the host and returned
// verbatim on each poll. The host encrypts it at rest.
type providerState struct {
	Code     string `json:"code"`
	IssuedAt int64  `json:"issued_at"`
}

func (s *Service) Start(context.Context, *pluginv1.WatchSyncDeviceAuthorizationServiceStartRequest) (*pluginv1.WatchSyncDeviceAuthorizationServiceStartResponse, error) {
	code, err := generateCode()
	if err != nil {
		return &pluginv1.WatchSyncDeviceAuthorizationServiceStartResponse{Fault: invalidRequestFault(err)}, nil
	}
	normalized := normalizeCode(code)
	formatted := formatCode(normalized)
	issued := now()
	state, err := json.Marshal(providerState{Code: normalized, IssuedAt: issued.Unix()})
	if err != nil {
		return &pluginv1.WatchSyncDeviceAuthorizationServiceStartResponse{Fault: invalidRequestFault(err)}, nil
	}
	return &pluginv1.WatchSyncDeviceAuthorizationServiceStartResponse{
		UserCode:                formatted,
		VerificationUrl:         bridgeBaseURL + "/",
		VerificationUrlComplete: bridgeBaseURL + "/connect?code=" + url.QueryEscape(formatted),
		ProviderState:           state,
		PollingInterval:         durationpb.New(pollingInterval),
		ExpiresAt:               timestamppb.New(issued.Add(codeValidity)),
	}, nil
}

func (s *Service) Poll(ctx context.Context, req *pluginv1.WatchSyncDeviceAuthorizationServicePollRequest) (*pluginv1.WatchSyncDeviceAuthorizationServicePollResponse, error) {
	var state providerState
	if err := json.Unmarshal(req.GetProviderState(), &state); err != nil {
		return invalidStateResponse(), nil
	}
	code := normalizeCode(state.Code)
	if code == "" {
		return invalidStateResponse(), nil
	}
	if now().After(time.Unix(state.IssuedAt, 0).Add(codeValidity)) {
		return &pluginv1.WatchSyncDeviceAuthorizationServicePollResponse{
			Status: pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_EXPIRED,
			Fault:  expiredFault("authorization window expired; start the connection again"),
		}, nil
	}

	poll, err := pollBridge(ctx, codeHash(code))
	if err != nil {
		// A transient bridge failure must not kill the flow; expires_at bounds
		// the host's retries.
		return &pluginv1.WatchSyncDeviceAuthorizationServicePollResponse{
			Status: pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_PENDING,
		}, nil
	}
	switch poll.Status {
	case "authorized":
		return s.authorized(ctx, code, poll.Envelope)
	case "expired":
		return &pluginv1.WatchSyncDeviceAuthorizationServicePollResponse{
			Status: pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_EXPIRED,
			Fault:  expiredFault("authorization window expired; start the connection again"),
		}, nil
	default:
		// "pending" and any unrecognized status both keep the host polling.
		return &pluginv1.WatchSyncDeviceAuthorizationServicePollResponse{
			Status: pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_PENDING,
		}, nil
	}
}

func (s *Service) authorized(ctx context.Context, code string, env *envelope) (*pluginv1.WatchSyncDeviceAuthorizationServicePollResponse, error) {
	key, err := deriveKey(code)
	if err != nil {
		return decryptFailureResponse(), nil
	}
	var token string
	if env != nil {
		token, err = decryptEnvelope(*env, key)
	}
	if err != nil || env == nil {
		return decryptFailureResponse(), nil
	}
	account, err := anilist.NewClient(token, nil).Viewer(ctx)
	if err != nil {
		fault := faultFromError(err)
		if fault.GetCode() == pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_CREDENTIAL {
			return &pluginv1.WatchSyncDeviceAuthorizationServicePollResponse{
				Status: pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_EXPIRED,
				Fault: &pluginv1.WatchSyncFault{
					Code:        pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_CREDENTIAL,
					SafeMessage: "AniList rejected the new credential; start the connection again",
				},
			}, nil
		}
		return &pluginv1.WatchSyncDeviceAuthorizationServicePollResponse{
			Status: pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_PENDING,
			Fault:  fault,
		}, nil
	}
	credentials := &pluginv1.WatchSyncCredentials{
		AccessToken:      token,
		TokenType:        "Bearer",
		SecretAttributes: map[string]string{"user_id": strconv.Itoa(account.ID)},
	}
	if expiresAt := anilist.TokenExpiresAt(token); !expiresAt.IsZero() {
		credentials.ExpiresAt = timestamppb.New(expiresAt)
	}
	return &pluginv1.WatchSyncDeviceAuthorizationServicePollResponse{
		Status:      pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_AUTHORIZED,
		Credentials: credentials,
		Account:     accountProto(account),
	}, nil
}

func generateCode() (string, error) {
	raw := make([]byte, codeLength)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	// byte%32 is uniform because 256 is divisible by 32, so no rejection
	// sampling is required.
	for i, b := range raw {
		raw[i] = codeAlphabet[b%32]
	}
	return string(raw), nil
}

func formatCode(normalized string) string {
	var b strings.Builder
	for i := 0; i < len(normalized); i += codeGroupSize {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(normalized[i : i+codeGroupSize])
	}
	return b.String()
}

func normalizeCode(code string) string {
	code = strings.ToUpper(code)
	code = strings.ReplaceAll(code, "-", "")
	return strings.NewReplacer("I", "1", "L", "1", "O", "0").Replace(code)
}

func accountProto(account anilist.Account) *pluginv1.WatchSyncAccount {
	return &pluginv1.WatchSyncAccount{
		ExternalSubject: strconv.Itoa(account.ID),
		Username:        account.Name,
		DisplayName:     account.Name,
		AvatarUrl:       account.AvatarURL,
		ProfileUrl:      account.ProfileURL,
	}
}

func invalidStateResponse() *pluginv1.WatchSyncDeviceAuthorizationServicePollResponse {
	return &pluginv1.WatchSyncDeviceAuthorizationServicePollResponse{
		Status: pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_EXPIRED,
		Fault: &pluginv1.WatchSyncFault{
			Code:        pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_REQUEST,
			SafeMessage: "authorization session is invalid; start the connection again",
		},
	}
}

func decryptFailureResponse() *pluginv1.WatchSyncDeviceAuthorizationServicePollResponse {
	return &pluginv1.WatchSyncDeviceAuthorizationServicePollResponse{
		Status: pluginv1.WatchSyncDeviceAuthorizationStatus_WATCH_SYNC_DEVICE_AUTHORIZATION_STATUS_EXPIRED,
		Fault:  expiredFault("stored authorization could not be read; start the connection again"),
	}
}

func expiredFault(message string) *pluginv1.WatchSyncFault {
	return &pluginv1.WatchSyncFault{
		Code:        pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_PERMANENT,
		SafeMessage: message,
	}
}

func invalidRequestFault(err error) *pluginv1.WatchSyncFault {
	return &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_REQUEST, SafeMessage: err.Error()}
}

func faultFromError(err error) *pluginv1.WatchSyncFault {
	fault := &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_TEMPORARY, SafeMessage: "temporary AniList request failure"}
	var apiErr *anilist.Error
	if !errors.As(err, &apiErr) {
		return fault
	}
	switch apiErr.Status {
	case http.StatusUnauthorized:
		fault.Code = pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_CREDENTIAL
		fault.SafeMessage = "AniList credentials are expired or revoked"
	case http.StatusForbidden:
		fault.Code = pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_PERMISSION_DENIED
		fault.SafeMessage = "AniList denied the request"
	case http.StatusTooManyRequests:
		fault.Code = pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_RATE_LIMITED
		fault.SafeMessage = "AniList rate limit reached"
		fault.RetryAfter = durationpb.New(apiErr.RetryAfter)
	default:
		if apiErr.Status >= 400 && apiErr.Status < 500 {
			fault.Code = pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_PERMANENT
			fault.SafeMessage = "AniList rejected the request"
		}
	}
	return fault
}
