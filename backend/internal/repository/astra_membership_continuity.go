package repository

import (
	"context"
	"crypto/sha256"
	"reflect"
	"slices"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func cloneAstraProofRevisions(prior map[string]time.Time, now time.Time) map[string]time.Time {
	result := make(map[string]time.Time)
	for revision, expires := range prior {
		if now.Before(expires) {
			result[revision] = expires
		}
	}
	return result
}

func carryAstraSurvivingProofs(old, next *codexGatewayPinUpstream, now time.Time) {
	if old == nil || !old.routingSettings.AutoQuality || !next.routingSettings.AutoQuality ||
		old.config.QualityMode != config.GatewayBorrowAccountQualityMode || next.config.QualityMode != config.GatewayBorrowAccountQualityMode || old.config.RotateNodes || next.config.RotateNodes {
		return
	}
	a, b := old.routingSettings, next.routingSettings
	a.Revision, b.Revision = "", ""
	a.CookiePool.SourceAccountIDs, b.CookiePool.SourceAccountIDs = nil, nil
	a.CookiePool.TargetAccountIDs, b.CookiePool.TargetAccountIDs = nil, nil
	a.CookiePool.TargetModels, b.CookiePool.TargetModels = nil, nil
	if !reflect.DeepEqual(a, b) {
		return
	}
	old.targetMu.Lock()
	defer old.targetMu.Unlock()
	old.mu.Lock()
	defer old.mu.Unlock()
	scopeUnchanged := func(id int64) bool {
		return slices.Contains(old.config.TargetAccountIDs, id) && slices.Contains(next.config.TargetAccountIDs, id) &&
			reflect.DeepEqual(old.config.ModelsForTarget(id), next.config.ModelsForTarget(id))
	}
	copyCheck := func(id int64, check astraTargetValidation) (astraTargetValidation, bool) {
		if !scopeUnchanged(id) || !check.passed || !now.Before(check.expires) || !now.Before(check.route.expires) ||
			check.route.cookie.Value == "" || check.cookieFingerprint != sha256.Sum256([]byte(check.route.cookie.Value)) ||
			!slices.Contains(next.config.SourceAccountIDs, check.sourceID) || !slices.Contains(old.config.SourceAccountIDs, check.sourceID) {
			return astraTargetValidation{}, false
		}
		// Copy only real surviving proof and its private route, never a candidate.
		check.compatibleRevisions = cloneAstraProofRevisions(check.compatibleRevisions, now)
		for _, revision := range []string{old.routingSettings.Revision, check.revision} {
			if revision == "" {
				continue
			}
			prior, exists := check.compatibleRevisions[revision]
			if exists && prior.Before(check.expires) {
				continue
			}
			if exists || len(check.compatibleRevisions) < 64 {
				check.compatibleRevisions[revision] = check.expires
			}
		}
		if next.routes == nil {
			next.routes = map[int64]codexGatewayRoute{}
		}
		if route, exists := next.routes[check.sourceID]; !exists || route.expires.Before(check.route.expires) {
			next.routes[check.sourceID] = check.route
		}
		if next.statuses == nil {
			next.statuses = map[int64]service.AstraRouteStatus{}
		}
		next.statuses[check.sourceID] = old.statuses[check.sourceID]
		if check.pairKey != ([32]byte{}) && old.targetPairPasses[check.pairKey] {
			if next.targetPairPasses == nil {
				next.targetPairPasses = map[[32]byte]bool{}
			}
			next.targetPairPasses[check.pairKey] = true
		}
		return check, true
	}
	for id, check := range old.targetChecks {
		if copied, allowed := copyCheck(id, check); allowed {
			next.setTargetCheck(id, "gpt-6-astra", copied)
		}
	}
	for key, check := range old.targetModelChecks {
		if copied, allowed := copyCheck(key.accountID, check); allowed {
			next.setTargetCheck(key.accountID, key.model, copied)
		}
	}
	for key, check := range old.targetRoutePasses {
		if copied, allowed := copyCheck(key.accountID, check); allowed {
			if next.targetRoutePasses == nil {
				next.targetRoutePasses = map[astraTargetRouteKey]astraTargetValidation{}
			}
			next.targetRoutePasses[key] = copied
			if current, exists := next.targetCheck(key.accountID, key.model); !exists || current.expires.Before(copied.expires) {
				next.setTargetCheck(key.accountID, key.model, copied)
			}
		}
	}
	for key, at := range old.targetSourceAttempts {
		if scopeUnchanged(key.accountID) && slices.Contains(next.config.SourceAccountIDs, key.sourceID) && now.Before(at.Add(time.Hour)) {
			if next.targetSourceAttempts == nil {
				next.targetSourceAttempts = map[astraTargetQuotaKey]time.Time{}
			}
			next.targetSourceAttempts[key] = at
		}
	}
	if slices.Contains(next.config.SourceAccountIDs, old.activeSource) {
		next.activeSource = old.activeSource
	}
}

// Only a previously passed proof explicitly carried into the current pool can
// authorize its exact prior revision. Deadline and membership remain unchanged.
func (s *astraRoutingUpstream) CodexGatewayPinWSBindingValidForRevision(ctx context.Context, id int64, model, cookie, revision string) bool {
	if !s.CodexGatewayPinWSBindingValidForModel(ctx, id, model, cookie) {
		return false
	}
	pool := s.current(ctx)
	if pool == nil {
		return false
	}
	pool.targetMu.Lock()
	defer pool.targetMu.Unlock()
	check, exists := pool.targetCheckForCookie(id, model, cookie)
	if !exists || !check.passed || !slices.Contains(pool.config.SourceAccountIDs, check.sourceID) {
		return false
	}
	if check.revision == revision {
		return time.Now().Before(check.expires)
	}
	expires, compatible := check.compatibleRevisions[revision]
	return compatible && time.Now().Before(expires) && time.Now().Before(check.expires)
}
