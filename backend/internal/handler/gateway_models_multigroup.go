package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// multiGroupModels filters each group's catalogue before merging. A denial in
// one group must not hide the same model when another bound group permits it.
func (h *GatewayHandler) multiGroupModels(c *gin.Context, groups []*service.APIKey, codex bool) {
	if codex && len(groups) > 0 && groups[0].ConcurrencyLimit > 0 {
		release, err := h.concurrencyHelper.AcquireAPIKeySlot(c.Request.Context(), groups[0].ID, groups[0].ConcurrencyLimit)
		if err != nil {
			h.handleConcurrencyError(c, err, "API key", false)
			return
		}
		defer release()
	}
	field, idField := "data", "id"
	if codex {
		field, idField = "models", "slug"
	}
	byID := make(map[string]json.RawMessage)
	envelope := map[string]json.RawMessage{}
	forced, _ := middleware.GetForcePlatformFromContext(c)
	for _, key := range groups {
		body, err := h.discoveryGroupCatalogue(c.Request.Context(), key, strings.TrimSpace(forced), codex, c.Query("client_version"))
		if err != nil {
			if c.Request.Context().Err() == nil {
				status := infraerrors.Code(err)
				if status < 400 {
					status = http.StatusBadGateway
				}
				writeOpenAIModelsError(c, status, "upstream_error", "Unable to load the complete model catalogue")
			}
			return
		}
		var groupEnvelope map[string]json.RawMessage
		if err := json.Unmarshal(body, &groupEnvelope); err != nil {
			writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue")
			return
		}
		// Preserve protocol envelope metadata from the first configured group.
		if len(envelope) == 0 {
			envelope = groupEnvelope
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(groupEnvelope[field], &entries); err != nil {
			writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue")
			return
		}
		for _, raw := range entries {
			var entry map[string]json.RawMessage
			var id string
			if json.Unmarshal(raw, &entry) != nil || json.Unmarshal(entry[idField], &id) != nil || id == "" {
				writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue")
				return
			}
			if !groupAllowsDiscoveryModel(key, id) {
				continue
			}
			if _, exists := byID[id]; !exists {
				byID[id] = raw
			}
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	entries := make([]json.RawMessage, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, byID[id])
	}
	envelope[field], _ = json.Marshal(entries)
	body, err := json.Marshal(envelope)
	if err != nil {
		writeOpenAIModelsError(c, http.StatusInternalServerError, "api_error", "Failed to encode model catalogue")
		return
	}
	// Source ETags and a representative group's denied list do not describe the union.
	c.Header("Cache-Control", "private, no-store")
	if !codex && c.Param("model") != "" {
		writeRetrievedModel(c, body)
		return
	}
	c.Data(http.StatusOK, "application/json", body)
}

func groupAllowsDiscoveryModel(key *service.APIKey, id string) bool {
	return (!key.Group.ModelAllowlistEnabled() || key.Group.ModelAllowlist.Allows(id)) && !service.UserGroupDeniesModel(key.DeniedModelsInGroup(), id)
}

func (h *GatewayHandler) discoveryGroupCatalogue(ctx context.Context, key *service.APIKey, forced string, codex bool, clientVersion string) ([]byte, error) {
	group := key.Group
	platform := forced
	if platform == "" {
		platform = group.Platform
	}
	if codex && platform == service.PlatformOpenAI && group.Platform == service.PlatformOpenAI {
		if h.openAIGatewayService == nil {
			return nil, fmt.Errorf("OpenAI model discovery is not configured")
		}
		manifest, _, err := h.openAIGatewayService.FetchGroupCodexModelsManifest(ctx, group, clientVersion, "", h.maxAccountSwitches)
		if err != nil {
			return nil, err
		}
		return manifest.Body, nil
	}
	if !codex && platform == service.PlatformOpenAI && group.Platform == service.PlatformOpenAI && group.CodexModelsManifestConfig.Enabled {
		if h.openAIGatewayService == nil {
			return nil, fmt.Errorf("OpenAI model discovery is not configured")
		}
		response, _, err := h.openAIGatewayService.FetchPinnedOpenAIModelsList(ctx, group, h.maxAccountSwitches, "")
		if err != nil {
			return nil, err
		}
		return response.Body, nil
	}
	var ids []string
	var err error
	if platform == service.PlatformComposite {
		endpoint := ""
		if codex {
			endpoint = service.CompositeRouteEndpointResponses
		}
		ids, err = h.compositeAvailableModelsForDiscovery(ctx, &group.ID, endpoint, true)
		if len(ids) == 0 {
			ids = service.DefaultModelIDsForPlatform(platform)
		}
	} else {
		ids, err = h.gatewayService.GetAvailableModelsForDiscovery(ctx, &group.ID, platform)
	}
	if err != nil {
		return nil, err
	}
	if codex {
		if len(ids) == 0 {
			ids = defaultCodexModelIDsForPlatform(platform)
		}
		if group.ModelAllowlistEnabled() {
			ids = group.ModelAllowlist.FilterForListing(ids)
		}
		ids = service.FilterCodexModelIDsForGroup(ids, group)
		ids = service.FilterUserGroupDeniedModelIDs(ids, key.DeniedModelsInGroup())
		return h.gatewayService.BuildCodexModelsManifestForGroup(ctx, group, forced, ids)
	}
	var entries any
	switch {
	case platform == service.PlatformDeepseek && group.Platform == service.PlatformDeepseek:
		entries = h.deepSeekModelEntries(ctx, group, ids)
	case group.ModelAllowlistEnabled():
		ids = group.ModelAllowlist.FilterForListing(service.ModelListingSource(platform, ids, service.DefaultModelIDsForPlatform(platform)))
		entries = modelsListEntries(platform, ids)
	case len(ids) > 0 || (platform == service.PlatformOpenAI && ids != nil):
		entries = modelsListEntries(platform, ids)
	case platform == service.PlatformOpenAI:
		entries = openai.DefaultModels
	case platform == service.PlatformGemini:
		entries = geminicli.DefaultModels
	case platform == service.PlatformGrok:
		entries = grokModelsListEntries(xai.DefaultModelIDs())
	case platform == service.PlatformTypeSafe:
		entries = modelsListEntries(platform, []string{typesafe.JevLatestModel})
	default:
		entries = claude.DefaultModels
	}
	return json.Marshal(gin.H{"object": "list", "data": entries})
}
