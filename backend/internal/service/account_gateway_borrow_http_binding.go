package service

import (
	"context"
	"sync"
	"time"
)

// Witness is populated only by the final verified transport immediately before
// network send. It is never accepted from a client header or JSON field.
type GatewayBorrowRequestWitness struct {
	mu        sync.Mutex
	model     string
	accountID int64
	cookie    string
	revision  string
	expires   time.Time
}
type gatewayBorrowWitnessContextKey struct{}

func WithGatewayBorrowRequestWitness(ctx context.Context) (context.Context, *GatewayBorrowRequestWitness) {
	w := &GatewayBorrowRequestWitness{}
	return context.WithValue(ctx, gatewayBorrowWitnessContextKey{}, w), w
}
func GatewayBorrowRequestWitnessFromContext(ctx context.Context) *GatewayBorrowRequestWitness {
	if ctx == nil {
		return nil
	}
	w, _ := ctx.Value(gatewayBorrowWitnessContextKey{}).(*GatewayBorrowRequestWitness)
	return w
}
func (w *GatewayBorrowRequestWitness) Confirm(accountID int64, model, cookie, revision string, expires time.Time) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.accountID, w.model, w.cookie, w.revision, w.expires = accountID, model, cookie, revision, expires
}
func (w *GatewayBorrowRequestWitness) snapshot() gatewayBorrowHTTPProof {
	if w == nil {
		return gatewayBorrowHTTPProof{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return gatewayBorrowHTTPProof{accountID: w.accountID, model: w.model, cookie: w.cookie, revision: w.revision, expires: w.expires}
}

type gatewayBorrowWireModelContextKey struct{}

func WithGatewayBorrowWireModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, gatewayBorrowWireModelContextKey{}, model)
}
func GatewayBorrowWireModelFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	if model, ok := ctx.Value(gatewayBorrowWireModelContextKey{}).(string); ok {
		return model, model != ""
	}
	scope, ok := ctx.Value(openAIFinalSendScopeKey{}).(openAIFinalSendScope)
	return scope.model, ok && scope.model != ""
}

type gatewayBorrowExpectedCookieContextKey struct{}

func GatewayBorrowExpectedCookieFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	cookie, _ := ctx.Value(gatewayBorrowExpectedCookieContextKey{}).(string)
	return cookie
}

type gatewayBorrowHTTPProof struct {
	accountID               int64
	model, cookie, revision string
	expires                 time.Time
	credential              [32]byte
	keyID, groupID          int64
}

var gatewayBorrowHTTPBindings = struct {
	sync.Mutex
	services map[*OpenAIGatewayService]map[string]gatewayBorrowHTTPProof
}{services: make(map[*OpenAIGatewayService]map[string]gatewayBorrowHTTPProof)}

func (s *OpenAIGatewayService) recordGatewayBorrowHTTPResponse(ctx context.Context, a *Account, responseID string, keyID, groupID int64, w *GatewayBorrowRequestWitness) {
	proof := w.snapshot()
	now := time.Now()
	if a == nil || responseID == "" || proof.accountID != a.ID || proof.cookie == "" || !now.Before(proof.expires) || !a.RequiresGatewayBorrowUpstream(proof.model) {
		return
	}
	proof.credential = gatewayBorrowCredentialFingerprint(a)
	proof.keyID, proof.groupID = keyID, groupID
	gatewayBorrowHTTPBindings.Lock()
	defer gatewayBorrowHTTPBindings.Unlock()
	entries := gatewayBorrowHTTPBindings.services[s]
	if entries == nil {
		entries = map[string]gatewayBorrowHTTPProof{}
		gatewayBorrowHTTPBindings.services[s] = entries
	}
	for id, p := range entries {
		if !now.Before(p.expires) {
			delete(entries, id)
		}
	}
	if len(entries) >= 10000 {
		return
	} // Saturated proof cache fails closed for new continuations.
	entries[responseID] = proof
}
func (s *OpenAIGatewayService) gatewayBorrowHTTPContinuation(ctx context.Context, a *Account, model, responseID string, keyID, groupID int64) (string, error) {
	gatewayBorrowHTTPBindings.Lock()
	proof, ok := gatewayBorrowHTTPBindings.services[s][responseID]
	gatewayBorrowHTTPBindings.Unlock()
	if !ok || s == nil || s.cfg == nil || proof.accountID != a.ID || proof.model != model || proof.keyID != keyID || proof.groupID != groupID || proof.credential != gatewayBorrowCredentialFingerprint(a) || !time.Now().Before(proof.expires) || proof.revision != s.cfg.AstraRouting(ctx).Revision {
		return "", denyOpenAITurn("gateway_borrow_continuation_unverified")
	}
	provider, ok := s.httpUpstream.(gatewayBorrowWSProvider)
	if !ok || !provider.CodexGatewayPinWSBindingValidForModel(ctx, a.ID, model, proof.cookie) {
		return "", denyOpenAITurn("gateway_borrow_continuation_unverified")
	}
	return proof.cookie, nil
}
