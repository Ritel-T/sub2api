package provider

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	SquarespaceOAuthAuthorizeEndpoint = "https://login.squarespace.com/api/1/login/oauth/provider/authorize"
	SquarespaceOAuthTokenEndpoint     = "https://login.squarespace.com/api/1/login/oauth/provider/tokens"
	SquarespaceOAuthReadScopes        = "website.orders.read,website.transactions.read"
)

// SquarespaceOAuthTokenPair is sensitive. Persist only through encrypted token
// storage, never put this type in a public DTO or plaintext provider config.
type SquarespaceOAuthTokenPair struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	AccessTokenExpiresAt  string `json:"access_token_expires_at"`
	RefreshTokenExpiresAt string `json:"refresh_token_expires_at"`
	TokenType             string `json:"token_type,omitempty"`
}

func (SquarespaceOAuthTokenPair) String() string   { return "SquarespaceOAuthTokenPair{redacted}" }
func (SquarespaceOAuthTokenPair) GoString() string { return "SquarespaceOAuthTokenPair{redacted}" }

func SquarespaceOAuthExpiry(raw string) (time.Time, error) {
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > float64(math.MaxInt64/1000000000) {
		return time.Time{}, squarespaceError("invalid_oauth_expiration")
	}
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(fraction*1e9)).UTC(), nil
}

func (p *SquarespaceOAuthTokenPair) Validate(now time.Time) error {
	if p == nil || !validSquarespaceToken(p.AccessToken) || !validSquarespaceToken(p.RefreshToken) ||
		(p.TokenType != "" && !strings.EqualFold(p.TokenType, "bearer")) {
		return squarespaceError("invalid_offline_oauth_pair")
	}
	a, errA := SquarespaceOAuthExpiry(p.AccessTokenExpiresAt)
	r, errR := SquarespaceOAuthExpiry(p.RefreshTokenExpiresAt)
	if errA != nil || errR != nil || !r.After(a) {
		return squarespaceError("invalid_oauth_pair_expirations")
	}
	if !now.IsZero() && (!a.After(now) || !r.After(now)) {
		return squarespaceError("expired_oauth_pair")
	}
	return nil
}

type SquarespaceOAuthClient struct {
	clientID, clientSecret string
	httpClient             *http.Client
}

func NewSquarespaceOAuthClient(clientID, clientSecret string) (*SquarespaceOAuthClient, error) {
	if !validSquarespaceToken(clientID) || !validSquarespaceToken(clientSecret) {
		return nil, squarespaceError("invalid_oauth_client_credentials")
	}
	return &SquarespaceOAuthClient{clientID: clientID, clientSecret: clientSecret, httpClient: &http.Client{
		Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (SquarespaceOAuthClient) String() string   { return "SquarespaceOAuthClient{redacted}" }
func (SquarespaceOAuthClient) GoString() string { return "SquarespaceOAuthClient{redacted}" }

// AuthorizeURL adds only offline access and the two read scopes. The caller must
// store/check state and register the complete callback URI with Squarespace.
func SquarespaceOfflineAuthorizeURL(clientID, redirectURI, state string) (string, error) {
	if !validSquarespaceToken(clientID) || len(state) < 32 || !squarespaceIDPattern.MatchString(state) {
		return "", squarespaceError("invalid_oauth_authorize_inputs")
	}
	parsed, err := url.Parse(redirectURI)
	if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Hostname() == "" ||
		(parsed.Scheme != "https" && !(parsed.Scheme == "http" && parsed.Hostname() == "localhost")) {
		return "", squarespaceError("invalid_oauth_redirect_uri")
	}
	query := url.Values{"client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {state}, "scope": {SquarespaceOAuthReadScopes}, "access_type": {"offline"}}
	return SquarespaceOAuthAuthorizeEndpoint + "?" + query.Encode(), nil
}

func (c *SquarespaceOAuthClient) RedeemCode(ctx context.Context, code, redirectURI string) (*SquarespaceOAuthTokenPair, error) {
	if !validSquarespaceToken(code) || redirectURI == "" {
		return nil, squarespaceError("invalid_oauth_code_inputs")
	}
	return c.request(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}})
}

// Refresh makes a single POST; it never retries an uncertain response. The
// service must hold a writer lease and persist/read back the complete new pair
// before first use of its access token (which invalidates the preceding RT).
func (c *SquarespaceOAuthClient) Refresh(ctx context.Context, refreshToken string) (*SquarespaceOAuthTokenPair, error) {
	if !validSquarespaceToken(refreshToken) {
		return nil, squarespaceError("invalid_refresh_token")
	}
	return c.request(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
}

func (c *SquarespaceOAuthClient) request(ctx context.Context, body url.Values) (*SquarespaceOAuthTokenPair, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, SquarespaceOAuthTokenEndpoint, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, squarespaceError("invalid_oauth_request")
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", squarespaceUserAgent)
	client := *c.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, squarespaceError("oauth_request_outcome_unknown")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		kind := "oauth_request_rejected"
		if res.StatusCode == 429 {
			kind = "rate_limited"
		}
		if res.StatusCode >= 300 && res.StatusCode < 400 {
			kind = "redirect_rejected"
		}
		return nil, &SquarespaceAPIError{Kind: kind, HTTPStatus: res.StatusCode, RetryAfter: squarespaceRetryAfter(res.Header.Get("Retry-After"))}
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, squarespaceError("invalid_oauth_response")
	}
	var pair SquarespaceOAuthTokenPair
	if err := json.Unmarshal(raw, &pair); err != nil {
		return nil, squarespaceError("invalid_oauth_response")
	}
	if err := pair.Validate(time.Now()); err != nil {
		return nil, err
	}
	return &pair, nil
}
