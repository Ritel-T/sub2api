package repository

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestAutomaticBorrowUnrelatedSourceRemovalPreservesLivePair(t *testing.T) {
	wrapper, pool := targetSourceOrderFixture(t, []int64{299, 298})
	settings := wrapper.cfg.AstraRouting(t.Context())
	shots := 0
	wrapper.delegate = gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		shots++
		return accountBorrowResponse("gpt-6-astra", "21"), nil
	}}
	request := accountBorrowRequest(t, "gpt-6-astra", "")
	pool.mu.Lock()
	selected := autoBorrowRouteSelection{298, pool.routes[298]}
	pool.mu.Unlock()
	_, _, _, release, err := wrapper.targetRouteOnce(request.Clone(context.WithValue(request.Context(), autoBorrowRouteKey{}, selected)), "exit", 300, 1, nil)
	release()
	require.NoError(t, err)
	require.Equal(t, 4, shots)
	cookie := selected.route.cookie.Value
	deadline := wrapper.CodexGatewayPinWSBindingExpiryForModel(t.Context(), 300, "gpt-6-astra", cookie)
	settings.Revision = "remove-unrelated-source"
	settings.CookiePool.SourceAccountIDs = []int64{298}
	wrapper.cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	next := wrapper.current(t.Context())
	require.NotSame(t, pool, next, "removed in-flight work cannot republish into this new pool")
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", cookie), "removing another donor cannot revoke this exact still-live proof")
	require.Equal(t, deadline, wrapper.CodexGatewayPinWSBindingExpiryForModel(t.Context(), 300, "gpt-6.1-sol", cookie))
	require.NoError(t, wrapper.VerifyAstraGatewayTarget(t.Context(), request, "exit", 300, 1))
	require.Equal(t, 4, shots, "a membership edit cannot demand another four-shot recovery")
}

func establishedContinuityPair(t *testing.T) (*astraRoutingUpstream, config.AstraRoutingSettings, string, time.Time) {
	wrapper, pool := targetSourceOrderFixture(t, []int64{299, 298})
	wrapper.delegate = gatewayPinDelegate{call: func(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
		return accountBorrowResponse("gpt-6-astra", "21"), nil
	}}
	request := accountBorrowRequest(t, "gpt-6-astra", "")
	selected := autoBorrowRouteSelection{298, pool.routes[298]}
	_, _, _, release, err := wrapper.targetRouteOnce(request.Clone(context.WithValue(request.Context(), autoBorrowRouteKey{}, selected)), "exit", 300, 1, nil)
	release()
	require.NoError(t, err)
	return wrapper, wrapper.cfg.AstraRouting(t.Context()), selected.route.cookie.Value, wrapper.CodexGatewayPinWSBindingExpiryForModel(t.Context(), 300, "gpt-6-astra", selected.route.cookie.Value)
}

func TestAstraMembershipContinuityRevokesMaterialChangesAndNeverReaddsOldProof(t *testing.T) {
	for _, change := range []string{"source_removed", "target_removed", "quality_mode", "ttl", "affinity", "rotate", "target_model", "global_model", "disabled"} {
		t.Run(change, func(t *testing.T) {
			wrapper, previous, cookie, _ := establishedContinuityPair(t)
			next := previous
			next.Revision = "material-change"
			switch change {
			case "source_removed":
				next.CookiePool.SourceAccountIDs = []int64{299}
			case "target_removed":
				next.CookiePool.TargetAccountIDs = []int64{301}
			case "quality_mode":
				next.QualityMode, next.CookiePool.QualityMode = "", ""
			case "ttl":
				next.CookiePool.TTLSeconds = 60
			case "affinity":
				next.CookiePool.IPAffinity = true
			case "rotate":
				next.CookiePool.RotateNodes = true
			case "target_model":
				next.CookiePool.TargetModels = map[string][]string{"300": {"gpt-6.1-sol"}}
			case "global_model":
				next.CookiePool.Models = []string{"gpt-6.1-sol"}
			case "disabled":
				next.CookiePool.Enabled = false
			}
			wrapper.ApplyAstraRoutingSettings(previous, next)
			require.False(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", cookie))
			if change == "source_removed" || change == "target_removed" {
				// No read of the intermediate revision: latest.prev is still the
				// removal revision, so the old pool cannot be carried across it.
				readded := previous
				readded.Revision = "readded"
				wrapper.ApplyAstraRoutingSettings(next, readded)
				require.False(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", cookie))
			}
		})
	}
}

func TestAstraMembershipPublicationFencesSkippedAndDelayedSettings(t *testing.T) {
	wrapper, a, cookie, _ := establishedContinuityPair(t)
	b := a
	b.Revision, b.CookiePool.SourceAccountIDs = "removed", []int64{299}
	wrapper.ApplyAstraRoutingSettings(a, b)
	c := a
	c.Revision = "readded-without-reading-b"
	wrapper.ApplyAstraRoutingSettings(b, c)
	// The loader still returns A, simulating a delayed request's stale read.
	require.Equal(t, c.Revision, wrapper.current(t.Context()).routingSettings.Revision)
	require.False(t, wrapper.CodexGatewayPinWSBindingValidForModel(t.Context(), 300, "gpt-6-astra", cookie), "a skipped deletion cannot revive the old proof")
}

func TestAstraMembershipContinuedRevisionsRetainOnlyExactProofDeadlines(t *testing.T) {
	wrapper, a, cookie, deadline := establishedContinuityPair(t)
	b := a
	b.Revision, b.CookiePool.SourceAccountIDs = "b", []int64{298}
	wrapper.ApplyAstraRoutingSettings(a, b)
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForRevision(t.Context(), 300, "gpt-6-astra", cookie, a.Revision))
	c := b
	c.Revision, c.CookiePool.SourceAccountIDs = "c", []int64{298, 297}
	wrapper.ApplyAstraRoutingSettings(b, c)
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForRevision(t.Context(), 300, "gpt-6-astra", cookie, a.Revision))
	require.True(t, wrapper.CodexGatewayPinWSBindingValidForRevision(t.Context(), 300, "gpt-6.1-sol", cookie, b.Revision))
	require.False(t, wrapper.CodexGatewayPinWSBindingValidForRevision(t.Context(), 300, "gpt-6-astra", cookie, "unknown"))
	require.False(t, wrapper.CodexGatewayPinWSBindingValidForRevision(t.Context(), 301, "gpt-6-astra", cookie, a.Revision))
	require.False(t, wrapper.CodexGatewayPinWSBindingValidForRevision(t.Context(), 300, "gpt-6-astra", "other-cookie", a.Revision))
	require.Equal(t, deadline, wrapper.CodexGatewayPinWSBindingExpiryForModel(t.Context(), 300, "gpt-6-astra", cookie))
	check, _ := wrapper.current(t.Context()).targetCheck(300, "gpt-6-astra")
	require.Equal(t, deadline, check.compatibleRevisions[a.Revision])
}
