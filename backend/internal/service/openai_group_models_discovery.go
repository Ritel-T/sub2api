package service

import (
	"context"
	"errors"
	"net/http"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// FetchGroupCodexModelsManifest is shared by single- and multi-group discovery.
// Client validators belong to the final group-specific or merged representation.
func (s *OpenAIGatewayService) FetchGroupCodexModelsManifest(ctx context.Context, group *Group, clientVersion, ifNoneMatch string, maxAccountSwitches int) (*OpenAIModelsResponse, *Account, error) {
	if group.Platform == PlatformOpenAI && group.CodexModelsManifestConfig.Enabled {
		manifest, account, err := s.FetchPinnedCodexModelsManifest(ctx, group, clientVersion)
		if err == nil {
			if err := s.MergeGroupConfiguredCodexModels(ctx, group, manifest, ifNoneMatch); err != nil {
				return nil, account, infraerrors.New(http.StatusInternalServerError, "api_error", "Failed to build Codex models manifest").WithCause(err)
			}
			return manifest, account, nil
		}
		if ctx.Err() != nil {
			return nil, account, ctx.Err()
		}
		if !group.CodexModelsManifestConfig.FallbackToScheduler {
			if errors.Is(err, ErrNoPinnedCodexModelsAccounts) {
				err = infraerrors.ServiceUnavailable("upstream_error", "No available pinned OpenAI accounts")
			}
			return nil, account, err
		}
	}
	if !group.CodexModelsManifestConfig.Enabled {
		manifest, configured, err := s.BuildGroupConfiguredCodexModelsManifest(ctx, group, ifNoneMatch)
		if err != nil {
			return nil, nil, infraerrors.New(http.StatusInternalServerError, "api_error", "Failed to build Codex models manifest").WithCause(err)
		}
		if configured {
			return manifest, nil, nil
		}
	}
	if maxAccountSwitches <= 0 {
		maxAccountSwitches = 3
	}
	failed := make(map[int64]struct{})
	var lastErr error
	for attempts := 0; ; attempts++ {
		account, err := s.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "", failed)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			if lastErr != nil {
				return nil, nil, lastErr
			}
			return nil, nil, infraerrors.ServiceUnavailable("upstream_error", "No available OpenAI accounts")
		}
		manifest, err := s.FetchCodexModelsManifest(ctx, account, clientVersion, "")
		if err != nil {
			if ctx.Err() != nil {
				return nil, account, ctx.Err()
			}
			if IsRetryableCodexModelsManifestError(err) && attempts < maxAccountSwitches {
				failed[account.ID] = struct{}{}
				lastErr = err
				continue
			}
			return nil, account, err
		}
		if err := s.CompleteAPIKeyCodexModelsManifestForClient(manifest, account); err != nil {
			return nil, account, infraerrors.New(http.StatusInternalServerError, "api_error", "Failed to complete Codex models manifest").WithCause(err)
		}
		if err := ApplyPinnedCodexModelsMapping(manifest, account, group); err != nil {
			return nil, account, infraerrors.New(http.StatusInternalServerError, "api_error", "Failed to apply model mappings").WithCause(err)
		}
		if err := s.MergeGroupConfiguredCodexModels(ctx, group, manifest, ifNoneMatch); err != nil {
			return nil, account, infraerrors.New(http.StatusInternalServerError, "api_error", "Failed to build Codex models manifest").WithCause(err)
		}
		return manifest, account, nil
	}
}
