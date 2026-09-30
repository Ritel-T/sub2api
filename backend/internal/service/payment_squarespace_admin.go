package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ImportSquarespaceOAuth verifies the incoming access token's website before
// atomically persisting the OAuth client and rotating token pair encrypted.
func (s *PaymentConfigService) ImportSquarespaceOAuth(ctx context.Context, websiteID, clientID, clientSecret string, expectedVersion int64, pair *provider.SquarespaceOAuthTokenPair) error {
	if websiteID == "" || clientID == "" || clientSecret == "" || expectedVersion < 0 || pair == nil {
		return infraerrors.BadRequest("INVALID_SQUARESPACE_OAUTH_IMPORT", "complete OAuth client and token pair are required")
	}
	for _, expiry := range []string{pair.AccessTokenExpiresAt, pair.RefreshTokenExpiresAt} {
		seconds, err := strconv.ParseFloat(expiry, 64)
		if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > 253402300799 || seconds <= float64(time.Now().Unix()+60) {
			return infraerrors.BadRequest("INVALID_SQUARESPACE_OAUTH_IMPORT", "OAuth tokens are expired or expire too soon")
		}
	}
	incoming := provider.SquarespaceTokenSourceFunc(func(context.Context) (string, error) { return pair.AccessToken, nil })
	client, err := provider.NewSquarespaceClient(websiteID, provider.SquarespaceReferenceFieldLabel, incoming)
	if err != nil {
		return infraerrors.BadRequest("INVALID_SQUARESPACE_OAUTH_IMPORT", "invalid Squarespace website")
	}
	if err = client.VerifyWebsite(ctx); err != nil {
		return infraerrors.BadRequest("SQUARESPACE_OAUTH_WEBSITE_MISMATCH", "incoming OAuth token could not verify the requested website")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin OAuth import transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	transactional := *s
	transactional.entClient = tx.Client()
	if err = transactional.importVerifiedSquarespaceOAuthPair(ctx, websiteID, clientID, clientSecret, expectedVersion, pair); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit OAuth import: %w", err)
	}

	return nil
}

// Called only after official website verification; kept separate for deterministic
// database/rotation tests which must never send token-bearing network requests.
func (s *PaymentConfigService) importVerifiedSquarespaceOAuthPair(ctx context.Context, websiteID, clientID, clientSecret string, expectedVersion int64, pair *provider.SquarespaceOAuthTokenPair) error {
	if err := s.lockSquarespaceOAuthImport(ctx, websiteID, clientID, expectedVersion, pair); err != nil {
		return err
	}
	if err := s.SaveSquarespaceOAuthCredentials(ctx, websiteID, clientID, clientSecret); err != nil {
		return err
	}
	return s.SaveSquarespaceOAuthTokens(ctx, websiteID, clientID, expectedVersion, pair)
}
