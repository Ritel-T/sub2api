package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect"

	"github.com/Wei-Shaw/sub2api/internal/payment"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentsyncstate"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
)

var ErrSquarespaceReauthorizationRequired = errors.New("squarespace OAuth reauthorization required")
var ErrSquarespaceTokenRotationBusy = errors.New("squarespace OAuth token rotation in progress")

type squarespaceRotationFailure struct{ code string }

func (e *squarespaceRotationFailure) Error() string { return e.code }
func (e *squarespaceRotationFailure) Unwrap() error { return ErrSquarespaceReauthorizationRequired }

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

// SaveSquarespaceOAuthTokens is the bootstrap/recovery write path. A caller
// cannot overwrite an active refresh writer; unknown recovery needs a new RT.
func (s *PaymentConfigService) SaveSquarespaceOAuthTokens(ctx context.Context, websiteID, clientID string, expectedVersion int64, pair *provider.SquarespaceOAuthTokenPair) error {
	return s.saveSquarespaceOAuthTokenPair(ctx, websiteID, clientID, expectedVersion, pair, nil)
}

type squarespaceRotationOwner struct {
	Version   int64
	StartedAt time.Time
}

func (s *PaymentConfigService) saveRotatedSquarespaceOAuthTokens(ctx context.Context, websiteID, clientID string, owner squarespaceRotationOwner, pair *provider.SquarespaceOAuthTokenPair) error {
	return s.saveSquarespaceOAuthTokenPair(ctx, websiteID, clientID, owner.Version, pair, &owner)
}
func (s *PaymentConfigService) saveSquarespaceOAuthTokenPair(ctx context.Context, websiteID, clientID string, expectedVersion int64, pair *provider.SquarespaceOAuthTokenPair, owner *squarespaceRotationOwner) error {
	if err := validateSquarespaceTokenPair(pair); err != nil {
		return err
	}
	if clientID == "" || len(clientID) > 200 || expectedVersion < 0 {
		return errors.New("invalid Squarespace OAuth client or version")
	}
	row, err := s.squarespaceSyncState(ctx, websiteID)
	if err != nil {
		return err
	}
	if err = s.validateSquarespaceTokenWriter(row, clientID, expectedVersion, pair, owner); err != nil {
		return err
	}
	plain, err := json.Marshal(pair)
	if err != nil {
		return errors.New("serialize Squarespace OAuth token pair")
	}
	encrypted, err := s.EncryptPrivatePaymentData(string(plain))
	if err != nil {
		return errors.New("encrypt Squarespace OAuth token pair")
	}
	update := s.entClient.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(row.ID), paymentsyncstate.TokenVersionEQ(expectedVersion), paymentsyncstate.Or(paymentsyncstate.OauthClientIDEQ(""), paymentsyncstate.OauthClientIDEQ(clientID)))
	if owner == nil {
		update = update.Where(paymentsyncstate.RotationPhaseEQ(row.RotationPhase))
	} else {
		update = update.Where(paymentsyncstate.RotationPhaseEQ("request_in_flight"), paymentsyncstate.RotationStartedAtEQ(owner.StartedAt))
	}
	count, err := update.SetOauthClientID(clientID).SetEncryptedOauthTokens(encrypted).SetTokenVersion(expectedVersion + 1).SetRotationPhase("idle").ClearRotationStartedAt().SetLastErrorCode("").ClearRetryAt().Save(ctx)
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("squarespace OAuth token writer changed")
	}
	stored, version, err := s.LoadSquarespaceOAuthTokens(ctx, websiteID)
	if err != nil || version != expectedVersion+1 || stored == nil || *stored != *pair {
		return errors.New("squarespace OAuth token persistence could not be verified")
	}
	return nil
}
func (s *PaymentConfigService) validateSquarespaceTokenWriter(row *dbent.PaymentSyncState, clientID string, expectedVersion int64, pair *provider.SquarespaceOAuthTokenPair, owner *squarespaceRotationOwner) error {
	if row == nil || row.TokenVersion != expectedVersion {
		return errors.New("squarespace OAuth token version changed")
	}
	if row.OauthClientID != "" && row.OauthClientID != clientID {
		return errors.New("squarespace website belongs to another OAuth client")
	}
	if owner != nil {
		if owner.Version != expectedVersion || row.RotationPhase != "request_in_flight" || row.RotationStartedAt == nil || !row.RotationStartedAt.Equal(owner.StartedAt) {
			return ErrSquarespaceTokenRotationBusy
		}
		return nil
	}
	if row.RotationPhase == "request_in_flight" {
		return ErrSquarespaceTokenRotationBusy
	}
	if row.RotationPhase != "idle" && row.RotationPhase != "unknown" {
		return ErrSquarespaceReauthorizationRequired
	}
	if row.RotationPhase == "unknown" {
		if err := pair.Validate(time.Now()); err != nil {
			return ErrSquarespaceReauthorizationRequired
		}
		if row.EncryptedOauthTokens != "" {
			plain, err := s.DecryptPrivatePaymentData(row.EncryptedOauthTokens)
			if err != nil {
				return ErrSquarespaceReauthorizationRequired
			}
			var previous provider.SquarespaceOAuthTokenPair
			if json.Unmarshal([]byte(plain), &previous) != nil || previous.RefreshToken == pair.RefreshToken {
				return ErrSquarespaceReauthorizationRequired
			}
		}
	}
	return nil
}

// Import holds this row lock before credentials or token writes. Even a stale
// request_in_flight marker cannot be stolen: timeout alone cannot prove outcome.
func (s *PaymentConfigService) lockSquarespaceOAuthImport(ctx context.Context, websiteID, clientID string, expectedVersion int64, pair *provider.SquarespaceOAuthTokenPair) error {
	row, err := s.squarespaceSyncState(ctx, websiteID)
	if err != nil {
		return err
	}
	query := s.entClient.PaymentSyncState.Query().Where(paymentsyncstate.IDEQ(row.ID))
	if s.entClient.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	row, err = query.Only(ctx)
	if err != nil {
		return err
	}
	return s.validateSquarespaceTokenWriter(row, clientID, expectedVersion, pair, nil)
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
	startedAt := s.now().UTC().Truncate(time.Microsecond)
	claimed, err := s.config.entClient.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(row.ID), paymentsyncstate.TokenVersionEQ(version), paymentsyncstate.RotationPhaseEQ("idle")).SetRotationPhase("request_in_flight").SetRotationStartedAt(startedAt).Save(ctx)
	if err != nil {
		return "", err
	}
	if claimed != 1 {
		return "", ErrSquarespaceTokenRotationBusy
	}
	persisted, err := s.config.entClient.PaymentSyncState.Get(ctx, row.ID)
	if err != nil || persisted.RotationStartedAt == nil || persisted.TokenVersion != version || persisted.RotationPhase != "request_in_flight" {
		code := "OAUTH_ROTATION_LEASE_READ_FAILED"
		s.markSquarespaceRotationUnknown(row.ID, squarespaceRotationOwner{Version: version, StartedAt: startedAt}, code)
		return "", &squarespaceRotationFailure{code: code}
	}
	owner := squarespaceRotationOwner{Version: version, StartedAt: *persisted.RotationStartedAt}
	refresh := s.refresh
	if refresh == nil {
		refresh = s.oauth.Refresh
	}
	refreshed, err := refresh(ctx, pair.RefreshToken)
	if err != nil {
		code := provider.SquarespaceOAuthFailureCode(err)
		s.markSquarespaceRotationUnknown(row.ID, owner, code)
		return "", &squarespaceRotationFailure{code: code}
	}
	if refreshed == nil || refreshed.RefreshToken == pair.RefreshToken || refreshed.Validate(s.now()) != nil {
		code := "OAUTH_ROTATION_INVALID_PAIR"
		s.markSquarespaceRotationUnknown(row.ID, owner, code)
		return "", &squarespaceRotationFailure{code: code}
	}
	if err = s.config.saveRotatedSquarespaceOAuthTokens(ctx, s.websiteID, s.clientID, owner, refreshed); err != nil {
		code := "OAUTH_ROTATION_PERSISTENCE_FAILED"
		s.markSquarespaceRotationUnknown(row.ID, owner, code)
		return "", &squarespaceRotationFailure{code: code}
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
		return errors.New("squarespace website belongs to another OAuth client")
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
		return errors.New("squarespace OAuth client binding changed")
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

func (s *squarespaceDurableTokenSource) markSquarespaceRotationUnknown(stateID int64, owner squarespaceRotationOwner, code string) {
	failCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = s.config.entClient.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(stateID), paymentsyncstate.TokenVersionEQ(owner.Version), paymentsyncstate.RotationPhaseEQ("request_in_flight"), paymentsyncstate.RotationStartedAtEQ(owner.StartedAt)).SetRotationPhase("unknown").SetLastErrorCode(code).Save(failCtx)
}
