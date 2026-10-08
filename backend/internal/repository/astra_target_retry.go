package repository

import (
	"context"
	"crypto/sha256"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"time"
)

type astraTargetPreparationKey struct{}
type astraTargetPreparationScope struct {
	accountID       int64
	quota, negative astraTargetQuotaKey
}

func targetPreparationContext(req *http.Request, proxy string, id int64, model string, profile *tlsfingerprint.Profile) context.Context {
	quota, negative := targetBorrowQuietKeys(req, id, model, proxy, 0, profile)
	return context.WithValue(req.Context(), astraTargetPreparationKey{}, astraTargetPreparationScope{id, quota, negative})
}

func laterTargetRetry(current, candidate astraTargetQuotaBackoff, now time.Time) astraTargetQuotaBackoff {
	if now.Before(candidate.until) && candidate.until.After(current.until) {
		return candidate
	}
	return current
}

// Called with targetMu and mu held. Use the existing exit/identity/source quiet
// periods even when a donor has supplied a replacement Cookie. A donor whose
// actual exit is not known remains eligible for the bounded exploration budget.
func (s *astraRoutingUpstream) effectiveTargetRetry(pool *codexGatewayPinUpstream, check astraTargetValidation, id int64, model string, source int64, route codexGatewayRoute, now time.Time) astraTargetQuotaBackoff {
	proxy := check.negativeKey.proxy
	if pool.config.RotateNodes {
		if route.node.Identity == "" {
			return astraTargetQuotaBackoff{}
		}
		proxy = sha256.Sum256([]byte(route.node.Identity))
	} else if pool.config.IPAffinity {
		if route.cookie.Value == "" {
			if check.sourceID != source || check.route.cookie.Value == "" {
				return astraTargetQuotaBackoff{}
			}
			route = check.route
		}
		proxy = sha256.Sum256([]byte(route.proxy))
	}
	quiet := astraTargetQuotaBackoff{}
	if check.quotaKey.identity != ([32]byte{}) {
		quota := check.quotaKey
		quota.proxy, quota.model = proxy, model
		if prior, waiting := s.targetQuotaWait(quota, now); waiting {
			quiet = laterTargetRetry(quiet, prior, now)
		}
		quota.model = pool.qualityModel(model)
		if prior, waiting := s.targetQuotaWait(quota, now); waiting {
			quiet = laterTargetRetry(quiet, prior, now)
		}
	}
	if check.negativeKey.identity != ([32]byte{}) {
		negative := check.negativeKey
		negative.proxy, negative.model, negative.sourceID = proxy, pool.qualityModel(model), source
		if prior, waiting := s.targetQuotaWait(negative, now); waiting {
			quiet = laterTargetRetry(quiet, prior, now)
		}
	}
	key := astraTargetRouteKey{id, pool.qualityModel(model), source, sha256.Sum256([]byte(route.cookie.Value))}
	if failure, exists := pool.targetRouteFailures[key]; exists && failure.negativeKey.identity == check.negativeKey.identity &&
		(failure.negativeKey.identity == ([32]byte{}) || failure.negativeKey.proxy == proxy) {
		quiet = laterTargetRetry(quiet, astraTargetQuotaBackoff{until: failure.retryAfter, reason: failure.reason}, now)
	}
	// Manually configured legacy pools/tests may predate stored quiet keys.
	if check.negativeKey.identity == ([32]byte{}) && !check.passed && check.sourceID == source {
		quiet = laterTargetRetry(quiet, astraTargetQuotaBackoff{until: check.retryAfter, reason: check.reason}, now)
	}
	return quiet
}

// Return a deadline only when every configured donor is currently known to be
// unusable. The earliest donor can then resume; one unknown/new donor permits
// exploration and never converts a missing proof into a pass.
func (s *astraRoutingUpstream) targetRetryDeadline(pool *codexGatewayPinUpstream, check astraTargetValidation, id int64, model string, now time.Time) astraTargetQuotaBackoff {
	earliest := astraTargetQuotaBackoff{}
	for _, source := range pool.config.SourceAccountIDs {
		retry := s.effectiveTargetRetry(pool, check, id, model, source, pool.routes[source], now)
		if !now.Before(retry.until) {
			return astraTargetQuotaBackoff{}
		}
		if earliest.until.IsZero() || retry.until.Before(earliest.until) {
			earliest = retry
		}
	}
	return earliest
}

// Source renewal is useful only for a target that can validate on this donor,
// or for preserving a still-valid proven route. No new probe/cache participates.
func (s *astraRoutingUpstream) sourceHasTargetDemand(ctx context.Context, pool *codexGatewayPinUpstream, source int64, route codexGatewayRoute, now time.Time) bool {
	scope, currentIdentity := ctx.Value(astraTargetPreparationKey{}).(astraTargetPreparationScope)
	pool.targetMu.Lock()
	defer pool.targetMu.Unlock()
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for _, id := range pool.config.TargetAccountIDs {
		seen := map[string]bool{}
		for _, model := range pool.config.ModelsForTarget(id) {
			model = pool.qualityModel(model)
			if seen[model] {
				continue
			}
			seen[model] = true
			check, exists := pool.targetCheck(id, model)
			if !exists {
				return true
			}
			if currentIdentity && scope.accountID == id {
				// Account reauthentication may have replaced the last checked identity.
				// Use the caller's current hashes without retaining any credentials.
				check.quotaKey, check.negativeKey = scope.quota, scope.negative
			}
			if check.passed && check.sourceID == source && now.Before(check.expires) {
				return true
			}
			if !now.Before(s.effectiveTargetRetry(pool, check, id, model, source, route, now).until) {
				return true
			}
		}
	}
	return false
}

// Cached quality proof skips a new Astra probe, but the requested business
// model still owns its independent credential/exit quota quiet period.
func (s *astraRoutingUpstream) readyTargetQuota(check astraTargetValidation, model string, now time.Time) astraTargetQuotaBackoff {
	if check.quotaKey.identity == ([32]byte{}) {
		return astraTargetQuotaBackoff{}
	}
	quota := check.quotaKey
	quota.model = model
	if prior, waiting := s.targetQuotaWait(quota, now); waiting {
		return prior
	}
	return astraTargetQuotaBackoff{}
}
