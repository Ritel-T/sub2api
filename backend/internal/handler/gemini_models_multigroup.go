package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/gemini"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type geminiModelsFailure struct {
	status     int
	message    string
	routingErr error
}

func (f *geminiModelsFailure) write(c *gin.Context) {
	if f.routingErr != nil {
		markOpsRoutingCapacityLimitedIfNoAvailable(c, f.routingErr)
	}
	googleError(c, f.status, f.message)
}

func geminiDiscoveryModelAllowed(key *service.APIKey, model string) bool {
	return (key.Group == nil || !key.Group.ModelAllowlistEnabled() || key.Group.ModelAllowlist.Allows(model)) &&
		!service.UserGroupDeniesModel(key.DeniedModelsInGroup(), model)
}

// loadGeminiModels acquires one group's native catalog without writing an HTTP
// response. Single-group calls retain upstream metadata, envelopes and headers.
// Complete discovery also propagates account-catalog query failures.
func (h *GatewayHandler) loadGeminiModels(ctx context.Context, key *service.APIKey, forcePlatform string, complete bool) (*service.UpstreamHTTPResult, *geminiModelsFailure) {
	agIDs, err := h.geminiCompatService.AntigravityGeminiModelIDs(ctx, key.GroupID, forcePlatform != service.PlatformAntigravity)
	if err != nil {
		return nil, &geminiModelsFailure{status: http.StatusServiceUnavailable, message: "Unable to list Antigravity models"}
	}
	agModels := make([]gemini.Model, 0, len(agIDs))
	for _, id := range agIDs {
		agModels = append(agModels, gemini.FallbackModel(id))
	}
	staticResult := func(models []gemini.Model) *service.UpstreamHTTPResult {
		filtered := make([]gemini.Model, 0, len(models))
		for _, model := range models {
			if complete || geminiDiscoveryModelAllowed(key, model.Name) {
				filtered = append(filtered, model)
			}
		}
		body, _ := json.Marshal(gemini.ModelsListResponse{Models: filtered})
		return &service.UpstreamHTTPResult{StatusCode: http.StatusOK, Body: body, Headers: http.Header{"Content-Type": {"application/json"}}}
	}
	if forcePlatform == service.PlatformAntigravity {
		return staticResult(agModels), nil
	}

	declared, err := h.geminiCompatService.DeclaredModelIDs(ctx, key.GroupID)
	if err != nil && complete {
		return nil, &geminiModelsFailure{status: http.StatusServiceUnavailable, message: "Unable to list Gemini models"}
	}
	if err == nil && len(declared) > 0 {
		ids := append([]string{}, declared...)
		for _, id := range agIDs {
			if !slices.Contains(declared, id) {
				ids = append(ids, id)
			}
		}
		if key.Group != nil && key.Group.ModelAllowlistEnabled() {
			ids = key.Group.ModelAllowlist.FilterForListing(ids)
		}
		models := make([]gemini.Model, 0, len(ids))
		for _, id := range ids {
			models = append(models, gemini.FallbackModel(id))
		}
		return staticResult(models), nil
	}

	account, err := h.geminiCompatService.SelectAccountForModelsEndpoints(ctx, key.GroupID)
	if err != nil {
		// Preserve the documented mixed-only group fallback.
		if len(agModels) > 0 {
			return staticResult(agModels), nil
		}
		return nil, &geminiModelsFailure{status: http.StatusServiceUnavailable, message: "No available Gemini accounts: " + err.Error(), routingErr: err}
	}
	release, err := h.concurrencyHelper.AcquireAPIKeySlot(ctx, key.ID, key.ConcurrencyLimit)
	if err != nil {
		status, _, _, message := concurrencyErrorResponse(err, "API key")
		return nil, &geminiModelsFailure{status: status, message: message}
	}
	defer release()
	res, err := h.geminiCompatService.ForwardListModels(ctx, account)
	if err != nil {
		return nil, &geminiModelsFailure{status: http.StatusBadGateway, message: err.Error()}
	}
	if shouldFallbackGeminiModels(res) {
		return staticResult(mergeGeminiModelLists(gemini.DefaultModels(), agModels)), nil
	}
	if complete && res.StatusCode == http.StatusOK {
		completeRes, failure := h.loadCompleteGeminiModelPages(ctx, account, res)
		if failure != nil {
			return nil, failure
		}
		// Continuation-page errors propagate without a partial success or fallback.
		res = completeRes
	}
	if res.StatusCode == http.StatusOK && len(agModels) > 0 {
		if merged, ok := appendUpstreamGeminiModels(res.Body, agModels); ok {
			res.Body = merged
		}
	}
	var allowlist service.GroupModelAllowlist
	if key.Group != nil {
		allowlist = key.Group.ModelAllowlist
	}
	if !complete && (allowlist.Enabled || len(key.DeniedModelsInGroup()) > 0) {
		if filtered, dropped, ok := filterGeminiModelsBody(res.Body, allowlist, key.DeniedModelsInGroup()); ok && dropped {
			res.Body = filtered
			res.Headers.Del("ETag")
			res.Headers.Del("Last-Modified")
		}
	}
	return res, nil
}

// Bound pagination by both the actual opaque token and page count. The same
// selected provider account and API-key admission cover the entire traversal.
func (h *GatewayHandler) loadCompleteGeminiModelPages(ctx context.Context, account *service.Account, first *service.UpstreamHTTPResult) (*service.UpstreamHTTPResult, *geminiModelsFailure) {
	const maxPages = 100
	res := first
	models := make([]json.RawMessage, 0)
	seenTokens := make(map[string]bool)
	for page := 0; page < maxPages; page++ {
		var envelope struct {
			Models        []json.RawMessage `json:"models"`
			NextPageToken string            `json:"nextPageToken"`
		}
		if res == nil || json.Unmarshal(res.Body, &envelope) != nil || envelope.Models == nil {
			return nil, &geminiModelsFailure{status: http.StatusBadGateway, message: "Unable to read a complete Gemini model catalog"}
		}
		models = append(models, envelope.Models...)
		if envelope.NextPageToken == "" {
			body, _ := json.Marshal(struct {
				Models []json.RawMessage `json:"models"`
			}{Models: models})
			return &service.UpstreamHTTPResult{StatusCode: http.StatusOK, Body: body}, nil
		}
		if seenTokens[envelope.NextPageToken] || page+1 == maxPages {
			return nil, &geminiModelsFailure{status: http.StatusBadGateway, message: "Gemini model catalog pagination did not terminate"}
		}
		seenTokens[envelope.NextPageToken] = true
		var err error
		res, err = h.geminiCompatService.ForwardListModelsPage(ctx, account, envelope.NextPageToken)
		if err != nil {
			return nil, &geminiModelsFailure{status: http.StatusBadGateway, message: err.Error()}
		}
		if res != nil && res.StatusCode != http.StatusOK {
			return res, nil
		}
	}
	return nil, &geminiModelsFailure{status: http.StatusBadGateway, message: "Gemini model catalog pagination did not terminate"}
}

func nativeGeminiDiscoveryGroup(key *service.APIKey, forcePlatform string) bool {
	if key == nil || key.Group == nil {
		return false
	}
	if forcePlatform != "" {
		return forcePlatform == service.PlatformAntigravity && key.Group.Platform == service.PlatformAntigravity
	}
	// Direct Antigravity groups use the forced route. Composite's native
	// default is Gemini; mixed Antigravity contributions still require opt-in.
	return key.Group.Platform == service.PlatformGemini || key.Group.Platform == service.PlatformComposite
}

func normalizedGeminiDiscoveryName(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(name), "models/")
}

// parseCompleteGeminiCatalog retains each entire raw model object and only
// accepts complete catalogs produced by the loader (no outward continuation).
func parseCompleteGeminiCatalog(body []byte) ([]json.RawMessage, bool) {
	var envelope struct {
		Models        []json.RawMessage `json:"models"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.NextPageToken != "" || envelope.Models == nil {
		return nil, false
	}
	for _, raw := range envelope.Models {
		var model struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &model) != nil || normalizedGeminiDiscoveryName(model.Name) == "" {
			return nil, false
		}
	}
	return envelope.Models, true
}

// multiGroupGeminiModels serves both collection and retrieve from exactly the
// same allowed catalogs. It never borrows one candidate's conditional headers.
func (h *GatewayHandler) multiGroupGeminiModels(c *gin.Context, groups []*service.APIKey, requested string) {
	c.Header("Cache-Control", "no-store")
	c.Header("ETag", "")
	c.Header("Last-Modified", "")
	// The legacy native endpoint ignored client page parameters. Return the
	// complete union with that same query behavior and no invented cursor.
	forcePlatform, _ := middleware.GetForcePlatformFromContext(c)
	models := make([]json.RawMessage, 0)
	seen := make(map[string]bool)
	var found json.RawMessage
	compatible := false
	for _, key := range groups {
		if !nativeGeminiDiscoveryGroup(key, forcePlatform) {
			continue
		}
		compatible = true
		res, failure := h.loadGeminiModels(c.Request.Context(), key, forcePlatform, true)
		if failure != nil {
			failure.write(c)
			return
		}
		if res.StatusCode != http.StatusOK {
			if res.StatusCode >= http.StatusBadRequest {
				// Preserve the upstream error body without a group's validators.
				c.Data(res.StatusCode, "application/json", res.Body)
			} else {
				googleError(c, http.StatusBadGateway, "Unable to read a complete Gemini model catalog")
			}
			return
		}
		entries, ok := parseCompleteGeminiCatalog(res.Body)
		if !ok {
			googleError(c, http.StatusBadGateway, "Unable to read a complete Gemini model catalog")
			return
		}
		for _, raw := range entries {
			var model struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(raw, &model) // Validated by parseCompleteGeminiCatalog.
			if !geminiDiscoveryModelAllowed(key, model.Name) {
				continue
			}
			name := normalizedGeminiDiscoveryName(model.Name)
			if requested != "" && found == nil && geminiDiscoveryModelAllowed(key, requested) {
				if name == requested {
					found = raw
				} else if strings.Contains(name, "*") && service.DeclaredModelsCover([]string{name}, requested) {
					// Declared mapping patterns retain the single-group retrieve contract.
					found, _ = json.Marshal(gemini.FallbackModel(requested))
				}
			}
			if !seen[name] {
				seen[name] = true
				models = append(models, raw)
			}
		}
	}
	if !compatible {
		googleError(c, http.StatusBadRequest, "API key group platform is not gemini")
		return
	}
	if requested != "" {
		if found == nil {
			googleError(c, http.StatusNotFound, "models/"+requested+" is not found")
			return
		}
		c.Data(http.StatusOK, "application/json", found)
		return
	}
	sort.SliceStable(models, func(i, j int) bool {
		var a, b struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(models[i], &a)
		_ = json.Unmarshal(models[j], &b)
		return normalizedGeminiDiscoveryName(a.Name) < normalizedGeminiDiscoveryName(b.Name)
	})
	c.JSON(http.StatusOK, struct {
		Models []json.RawMessage `json:"models"`
	}{Models: models})
}

// multiGroupAntigravityModels is also used by GatewayHandler.AntigravityModels.
// That route exposes the existing static catalog for each authorized AG group.
func (h *GatewayHandler) multiGroupAntigravityModels(c *gin.Context, groups []*service.APIKey) {
	models := make([]antigravity.ClaudeModel, 0)
	seen := make(map[string]bool)
	for _, key := range groups {
		if key == nil || key.Group == nil || key.Group.Platform != service.PlatformAntigravity {
			continue
		}
		for _, model := range antigravity.DefaultModels() {
			if geminiDiscoveryModelAllowed(key, model.ID) && !seen[model.ID] {
				seen[model.ID] = true
				models = append(models, model)
			}
		}
	}
	sort.SliceStable(models, func(i, j int) bool {
		return normalizedGeminiDiscoveryName(models[i].ID) < normalizedGeminiDiscoveryName(models[j].ID)
	})
	c.Header("Cache-Control", "no-store")
	c.Header("ETag", "")
	c.Header("Last-Modified", "")
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": models})
}
