package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentsyncstate"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
)

var ErrSquarespaceReauthorizationRequired = errors.New("Squarespace OAuth reauthorization required")
var ErrSquarespaceTokenRotationBusy = errors.New("Squarespace OAuth token rotation in progress")

func (s *PaymentConfigService) squarespaceSyncState(ctx context.Context, websiteID string) (*dbent.PaymentSyncState, error) {
	if websiteID == "" || len(websiteID) > 128 {
		return nil, errors.New("invalid Squarespace website identifier")
	}
	row, err := s.entClient.PaymentSyncState.Query().Where(paymentsyncstate.ProviderKeyEQ("squarespace"), paymentsyncstate.WebsiteIDEQ(websiteID)).Only(ctx)
	if err == nil {
		return row, nil
	}
	if !dbent.IsNotFound(err) {
		return nil, err
	}
	row, err = s.entClient.PaymentSyncState.Create().SetProviderKey("squarespace").SetWebsiteID(websiteID).Save(ctx)
	if dbent.IsConstraintError(err) {
		return s.entClient.PaymentSyncState.Query().Where(paymentsyncstate.ProviderKeyEQ("squarespace"), paymentsyncstate.WebsiteIDEQ(websiteID)).Only(ctx)
	}
	return row, err
}

// LoadSquarespaceOAuthTokens returns the encrypted-at-rest pair and its CAS version.
// Tokens are never stored in provider snapshots, audit details or response DTOs.
func (s *PaymentConfigService) LoadSquarespaceOAuthTokens(ctx context.Context, websiteID string) (*provider.SquarespaceOAuthTokenPair, int64, error) {
	row, err := s.squarespaceSyncState(ctx, websiteID)
	if err != nil {
		return nil, 0, err
	}
	if row.EncryptedOauthTokens == "" {
		return nil, row.TokenVersion, ErrSquarespaceReauthorizationRequired
	}
	plain, err := s.DecryptPrivatePaymentData(row.EncryptedOauthTokens)
	if err != nil {
		return nil, row.TokenVersion, ErrSquarespaceReauthorizationRequired
	}
	var pair provider.SquarespaceOAuthTokenPair
	if json.Unmarshal([]byte(plain), &pair) != nil || validateSquarespaceTokenPair(&pair) != nil {
		return nil, row.TokenVersion, ErrSquarespaceReauthorizationRequired
	}
	return &pair, row.TokenVersion, nil
}

// SaveSquarespaceOAuthTokens atomically commits both rotating tokens before the
// new access token can be used. Concurrent clients cannot overwrite a newer pair.
func (s *PaymentConfigService) SaveSquarespaceOAuthTokens(ctx context.Context, websiteID, clientID string, expectedVersion int64, pair *provider.SquarespaceOAuthTokenPair) error {
	if err := validateSquarespaceTokenPair(pair); err != nil {
		return err
	}
	if clientID == "" || len(clientID) > 200 {
		return errors.New("invalid Squarespace OAuth client identifier")
	}
	row, err := s.squarespaceSyncState(ctx, websiteID)
	if err != nil {
		return err
	}
	if row.OauthClientID != "" && row.OauthClientID != clientID {
		return errors.New("Squarespace website belongs to another OAuth client")
	}
	plain, err := json.Marshal(pair)
	if err != nil {
		return errors.New("serialize Squarespace OAuth token pair")
	}
	encrypted, err := s.EncryptPrivatePaymentData(string(plain))
	if err != nil {
		return errors.New("encrypt Squarespace OAuth token pair")
	}
	count, err := s.entClient.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(row.ID), paymentsyncstate.TokenVersionEQ(expectedVersion), paymentsyncstate.Or(paymentsyncstate.OauthClientIDEQ(""), paymentsyncstate.OauthClientIDEQ(clientID))).SetOauthClientID(clientID).SetEncryptedOauthTokens(encrypted).SetTokenVersion(expectedVersion + 1).SetRotationPhase("idle").ClearRotationStartedAt().SetLastErrorCode("").Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("Squarespace OAuth token version changed")
	}
	stored, version, err := s.LoadSquarespaceOAuthTokens(ctx, websiteID)
	if err != nil || version != expectedVersion+1 || stored == nil || *stored != *pair {
		return errors.New("Squarespace OAuth token persistence could not be verified")
	}
	return nil
}

func validateSquarespaceTokenPair(pair *provider.SquarespaceOAuthTokenPair) error {
	if pair == nil || pair.Validate(time.Time{}) != nil {
		return ErrSquarespaceReauthorizationRequired
	}
	return nil
}
func squarespaceOAuthExpiry(value string) (time.Time, error) {
	return provider.SquarespaceOAuthExpiry(value)
}

type squarespaceDurableTokenSource struct {
	config    *PaymentConfigService
	websiteID string
	clientID  string
	oauth     *provider.SquarespaceOAuthClient
	now       func() time.Time
	refresh   func(context.Context, string) (*provider.SquarespaceOAuthTokenPair, error)
}

func (s *squarespaceDurableTokenSource) AccessToken(ctx context.Context) (string, error) {
	row, err := s.config.squarespaceSyncState(ctx, s.websiteID)
	if err != nil {
		return "", err
	}
	if row.OauthClientID != s.clientID {
		return "", ErrSquarespaceReauthorizationRequired
	}
	if row.RotationPhase != "idle" {
		if row.RotationPhase == "request_in_flight" && row.RotationStartedAt != nil && s.now().Sub(*row.RotationStartedAt) < 2*time.Minute {
			return "", ErrSquarespaceTokenRotationBusy
		}
		return "", ErrSquarespaceReauthorizationRequired
	}
	pair, version, err := s.config.LoadSquarespaceOAuthTokens(ctx, s.websiteID)
	if err != nil {
		return "", err
	}
	expires, _ := squarespaceOAuthExpiry(pair.AccessTokenExpiresAt)
	if expires.After(s.now().Add(45 * time.Second)) {
		return pair.AccessToken, nil
	}
	refreshExpires, _ := squarespaceOAuthExpiry(pair.RefreshTokenExpiresAt)
	if !refreshExpires.After(s.now().Add(30 * time.Second)) {
		return "", ErrSquarespaceReauthorizationRequired
	}
	claimed, err := s.config.entClient.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(row.ID), paymentsyncstate.TokenVersionEQ(version), paymentsyncstate.RotationPhaseEQ("idle")).SetRotationPhase("request_in_flight").SetRotationStartedAt(s.now()).Save(ctx)
	if err != nil {
		return "", err
	}
	if claimed != 1 {
		return "", ErrSquarespaceTokenRotationBusy
	}
	refresh := s.refresh
	if refresh == nil {
		refresh = s.oauth.Refresh
	}
	refreshed, err := refresh(ctx, pair.RefreshToken)
	if err != nil {
		// A refresh can succeed remotely even if the response was lost. Never replay
		// the old refresh token or use a new access token that was not durably stored.
		failCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = s.config.entClient.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(row.ID), paymentsyncstate.TokenVersionEQ(version), paymentsyncstate.RotationPhaseEQ("request_in_flight")).SetRotationPhase("unknown").SetLastErrorCode("OAUTH_ROTATION_UNKNOWN").Save(failCtx)
		return "", ErrSquarespaceReauthorizationRequired
	}
	if err := s.config.SaveSquarespaceOAuthTokens(ctx, s.websiteID, s.clientID, version, refreshed); err != nil {
		return "", fmt.Errorf("persist refreshed OAuth pair: %w", err)
	}
	return refreshed.AccessToken, nil
}

type squarespaceOAuthCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// SaveSquarespaceOAuthCredentials stores credentials only in the dedicated
// encrypted store. Public provider config and order snapshots never contain them.
func (s *PaymentConfigService) SaveSquarespaceOAuthCredentials(ctx context.Context, websiteID, clientID, clientSecret string) error {
	if clientID == "" || clientSecret == "" {
		return ErrSquarespaceReauthorizationRequired
	}
	row, err := s.squarespaceSyncState(ctx, websiteID)
	if err != nil {
		return err
	}
	if row.OauthClientID != "" && row.OauthClientID != clientID {
		return errors.New("Squarespace website belongs to another OAuth client")
	}
	plain, err := json.Marshal(squarespaceOAuthCredentials{ClientID: clientID, ClientSecret: clientSecret})
	if err != nil {
		return errors.New("serialize OAuth credentials")
	}
	encrypted, err := s.EncryptPrivatePaymentData(string(plain))
	if err != nil {
		return errors.New("encrypt OAuth credentials")
	}
	count, err := s.entClient.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(row.ID), paymentsyncstate.Or(paymentsyncstate.OauthClientIDEQ(""), paymentsyncstate.OauthClientIDEQ(clientID))).SetOauthClientID(clientID).SetEncryptedOauthCredentials(encrypted).Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("Squarespace OAuth client binding changed")
	}
	return nil
}

func (s *PaymentConfigService) squarespaceTokenSource(ctx context.Context, websiteID string) (*squarespaceDurableTokenSource, error) {
	row, err := s.squarespaceSyncState(ctx, websiteID)
	if err != nil {
		return nil, err
	}
	if row.EncryptedOauthCredentials == "" {
		return nil, ErrSquarespaceReauthorizationRequired
	}
	plain, err := s.DecryptPrivatePaymentData(row.EncryptedOauthCredentials)
	if err != nil {
		return nil, ErrSquarespaceReauthorizationRequired
	}
	var credentials squarespaceOAuthCredentials
	if json.Unmarshal([]byte(plain), &credentials) != nil || credentials.ClientID != row.OauthClientID {
		return nil, ErrSquarespaceReauthorizationRequired
	}
	oauth, err := provider.NewSquarespaceOAuthClient(credentials.ClientID, credentials.ClientSecret)
	if err != nil {
		return nil, ErrSquarespaceReauthorizationRequired
	}
	return &squarespaceDurableTokenSource{config: s, websiteID: websiteID, clientID: credentials.ClientID, oauth: oauth, now: time.Now}, nil
}

// createSquarespaceQueryProvider is the sole factory that injects managed OAuth
// into QueryOrder; CreatePayment only exposes the configured static hosted link.
func (s *PaymentService) createSquarespaceQueryProvider(ctx context.Context, instance *dbent.PaymentProviderInstance, cfg map[string]string) (payment.Provider, error) {
	if instance == nil {
		return nil, errors.New("missing Squarespace provider instance")
	}
	tokens, err := s.configService.squarespaceTokenSource(ctx, strings.TrimSpace(cfg["websiteId"]))
	if err != nil {
		return nil, err
	}
	return provider.NewSquarespaceWithTokenSource(strconv.FormatInt(instance.ID, 10), cfg, tokens)
}
