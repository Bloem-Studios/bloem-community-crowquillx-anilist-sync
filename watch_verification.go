package main

import (
	"context"
	"errors"
	"strings"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/crowquillx/silo-anilist-sync/anilist"
	"github.com/crowquillx/silo-anilist-sync/silostate"
)

const siloVerifiedCapability = "anilist-silo"

func (s *server) exchangeVerifiedCredential(ctx context.Context, req *pluginv1.WatchSyncExchangeAPIKeyRequest, token string) (*pluginv1.WatchSyncCredentialResponse, error) {
	config := req.GetProviderConfig()
	silo := silostate.Config{
		BaseURL:   strings.TrimSpace(config.GetValues()["silo.base_url"]),
		APIKey:    strings.TrimSpace(config.GetSecretValues()["silo.api_key"]),
		ProfileID: strings.TrimSpace(config.GetValues()["silo.profile_id"]),
	}
	if err := silostate.NewClient(silo).ValidateProfile(ctx); err != nil {
		return &pluginv1.WatchSyncCredentialResponse{Fault: siloVerificationFault(err)}, nil
	}
	response, err := credentialResponse(ctx, token, "Bearer", anilist.TokenExpiresAt(token))
	if err != nil || response.GetFault() != nil {
		return response, err
	}
	// The host forwards connection config only during API-key exchange. Keep
	// the verified scope with the encrypted credential, never in global config.
	attrs := response.Credentials.SecretAttributes
	attrs["silo.base_url"] = silo.BaseURL
	attrs["silo.api_key"] = silo.APIKey
	attrs["silo.profile_id"] = silo.ProfileID
	return response, nil
}

func verifyZeroStop(ctx context.Context, auth *pluginv1.WatchSyncAuthenticatedContext, event *pluginv1.WatchSyncEvent) (bool, error) {
	if auth.GetCapabilityId() != siloVerifiedCapability ||
		event.GetOperation() != pluginv1.WatchSyncOperation_WATCH_SYNC_OPERATION_SCROBBLE_STOP ||
		event.GetPositionSeconds() != 0 || event.GetCompletionPercent() != 0 ||
		event.GetDurationSeconds() <= 0 || event.GetMedia().GetMediaItemId() == "" {
		return false, nil
	}
	attrs := auth.GetCredentials().GetSecretAttributes()
	client := silostate.NewClient(silostate.Config{BaseURL: attrs["silo.base_url"], APIKey: attrs["silo.api_key"], ProfileID: attrs["silo.profile_id"]})
	return client.Played(ctx, event.GetMedia().GetMediaItemId())
}

func siloVerificationFault(err error) *pluginv1.WatchSyncFault {
	fault := &pluginv1.WatchSyncFault{Code: pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_TEMPORARY, SafeMessage: "Silo watched verification failed; try again"}
	var siloErr *silostate.Error
	if !errors.As(err, &siloErr) {
		return fault
	}
	fault.SafeMessage = siloErr.Error()
	switch siloErr.Kind {
	case silostate.InvalidConfig:
		fault.Code = pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_REQUEST
	case silostate.InvalidCredential:
		fault.Code = pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_CREDENTIAL
	case silostate.PermissionDenied:
		fault.Code = pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_PERMISSION_DENIED
	case silostate.RateLimited:
		fault.Code = pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_RATE_LIMITED
	case silostate.Permanent:
		fault.Code = pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_PERMANENT
	}
	return fault
}

func siloVerificationErrorResult(eventID string, err error) *pluginv1.WatchSyncApplyResult {
	fault := siloVerificationFault(err)
	result := &pluginv1.WatchSyncApplyResult{EventId: eventID, Fault: fault, Status: pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_RETRY}
	if fault.Code == pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_INVALID_REQUEST || fault.Code == pluginv1.WatchSyncFaultCode_WATCH_SYNC_FAULT_CODE_PERMANENT {
		result.Status = pluginv1.WatchSyncApplyStatus_WATCH_SYNC_APPLY_STATUS_REJECTED
	}
	return result
}
