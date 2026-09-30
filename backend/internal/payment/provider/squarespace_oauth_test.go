package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func squarespaceOAuthFixture() SquarespaceOAuthTokenPair {
	now := time.Now()
	return SquarespaceOAuthTokenPair{AccessToken: "MOCK_ACCESS_TOKEN", RefreshToken: "MOCK_REFRESH_TOKEN", AccessTokenExpiresAt: strconv.FormatInt(now.Add(30*time.Minute).Unix(), 10), RefreshTokenExpiresAt: strconv.FormatInt(now.Add(7*24*time.Hour).Unix(), 10), TokenType: "bearer"}
}

func TestSquarespaceOfflineAuthorizeUsesOnlyReadScopes(t *testing.T) {
	endpoint, err := SquarespaceOfflineAuthorizeURL("CLIENT", "http://localhost:8090/oauth/callback", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(endpoint)
	if parsed.Host != "login.squarespace.com" || parsed.Query().Get("access_type") != "offline" || parsed.Query().Get("scope") != "website.orders.read,website.transactions.read" || parsed.Query().Has("response_type") {
		t.Fatal("authorization requested unsupported permission/parameters")
	}
	if _, err := SquarespaceOfflineAuthorizeURL("CLIENT", "http://127.0.0.1:8090/oauth/callback", strings.Repeat("a", 64)); err == nil {
		t.Fatal("undocumented HTTP callback accepted")
	}
}

func TestSquarespaceOAuthExchangeAndRotationRequests(t *testing.T) {
	client, err := NewSquarespaceOAuthClient("CLIENT", "MOCK_CLIENT_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	requests := make([]url.Values, 0, 2)
	client.httpClient = &http.Client{Transport: squarespaceTestTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method != "POST" || request.URL.String() != SquarespaceOAuthTokenEndpoint || request.Header.Get("User-Agent") == "" {
			t.Fatal("unsafe OAuth request")
		}
		id, secret, ok := request.BasicAuth()
		if !ok || id != "CLIENT" || secret != "MOCK_CLIENT_SECRET" {
			t.Fatal("invalid OAuth Basic auth")
		}
		raw, _ := io.ReadAll(request.Body)
		fields, _ := url.ParseQuery(string(raw))
		requests = append(requests, fields)
		pair := squarespaceOAuthFixture()
		body, _ := json.Marshal(pair)
		return squarespaceTestResponse(200, string(body)), nil
	})}
	if _, err := client.RedeemCode(context.Background(), "MOCK_CODE", "http://localhost:8090/oauth/callback"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Refresh(context.Background(), "MOCK_OLD_REFRESH"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[0].Get("grant_type") != "authorization_code" || requests[0].Get("redirect_uri") != "http://localhost:8090/oauth/callback" || requests[1].Get("grant_type") != "refresh_token" || requests[1].Get("refresh_token") != "MOCK_OLD_REFRESH" {
		t.Fatal("wrong token grant parameters")
	}
	if len(requests[1]) != 2 {
		t.Fatal("added unsupported refresh parameters")
	}
}

func TestSquarespaceOAuthRejectsMissingOrExpiredRefreshAndRedactsDiagnostics(t *testing.T) {
	pair := squarespaceOAuthFixture()
	if strings.Contains(fmt.Sprintf("%v %+v %#v", pair, pair, pair), "MOCK_") {
		t.Fatal("OAuth pair leaked to fmt logging")
	}
	for _, test := range []struct {
		name   string
		change func(*SquarespaceOAuthTokenPair)
	}{
		{"missing_refresh", func(p *SquarespaceOAuthTokenPair) { p.RefreshToken = "" }},
		{"nan_expiry", func(p *SquarespaceOAuthTokenPair) { p.AccessTokenExpiresAt = "NaN" }},
		{"expired_access", func(p *SquarespaceOAuthTokenPair) { p.AccessTokenExpiresAt = "1" }},
		{"wrong_type", func(p *SquarespaceOAuthTokenPair) { p.TokenType = "unknown" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := pair
			test.change(&p)
			if err := p.Validate(time.Now()); err == nil {
				t.Fatal("unsafe OAuth pair accepted")
			}
		})
	}
}

func TestSquarespaceOAuthDoesNotRetryOrFollowRedirect(t *testing.T) {
	for _, status := range []int{302, 429, 401, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client, _ := NewSquarespaceOAuthClient("CLIENT", "MOCK_CLIENT_SECRET")
			calls := 0
			client.httpClient = &http.Client{Transport: squarespaceTestTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				response := squarespaceTestResponse(status, "MOCK_REFRESH_TOKEN")
				response.Header.Set("Location", "https://evil.example/")
				return response, nil
			})}
			_, err := client.Refresh(context.Background(), "MOCK_REFRESH_TOKEN")
			var upstream *SquarespaceAPIError
			if !errors.As(err, &upstream) || calls != 1 || strings.Contains(err.Error(), "MOCK_") {
				t.Fatalf("OAuth request was retried or leaked response: %v", err)
			}
		})
	}
}

func TestSquarespaceOAuthRefreshNumericEpochFields(t *testing.T) {
	client, err := NewSquarespaceOAuthClient("CLIENT", "MOCK_CLIENT_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	accessExpiry := strconv.FormatInt(time.Now().Add(30*time.Minute).Unix(), 10) + ".123456"
	refreshExpiry := strconv.FormatInt(time.Now().Add(7*24*time.Hour).Unix(), 10) + ".654321"
	response := fmt.Sprintf(`{"access_token":"MOCK_NEW_ACCESS","refresh_token":"MOCK_NEW_REFRESH","access_token_expires_at":%s,"refresh_token_expires_at":%s,"token_type":"bearer"}`, accessExpiry, refreshExpiry)
	calls := 0
	client.httpClient = &http.Client{Transport: squarespaceTestTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		return squarespaceTestResponse(200, response), nil
	})}
	pair, err := client.Refresh(context.Background(), "MOCK_OLD_REFRESH")
	if err != nil {
		t.Fatalf("valid numeric epoch response rejected: %v", err)
	}
	if pair.AccessTokenExpiresAt != accessExpiry || pair.RefreshTokenExpiresAt != refreshExpiry || calls != 1 {
		t.Fatal("numeric expirations were rounded, omitted, or request was retried")
	}
}

func TestSquarespaceOAuthEpochMixedJSONRepresentationsAndStrictRejections(t *testing.T) {
	accessExpiry := strconv.FormatInt(time.Now().Add(30*time.Minute).Unix(), 10) + ".123456"
	refreshExpiry := strconv.FormatInt(time.Now().Add(7*24*time.Hour).Unix(), 10) + ".654321"
	for _, test := range []struct{ name, access, refresh string }{
		{"both_strings", strconv.Quote(accessExpiry), strconv.Quote(refreshExpiry)},
		{"access_number", accessExpiry, strconv.Quote(refreshExpiry)},
		{"refresh_number", strconv.Quote(accessExpiry), refreshExpiry},
		{"both_numbers", accessExpiry, refreshExpiry},
		{"pretty_number_whitespace", " \n " + accessExpiry + " \t", refreshExpiry},
	} {
		t.Run(test.name, func(t *testing.T) {
			var pair SquarespaceOAuthTokenPair
			raw := fmt.Sprintf(`{"access_token":"MOCK_ACCESS","refresh_token":"MOCK_REFRESH","access_token_expires_at":%s,"refresh_token_expires_at":%s}`, test.access, test.refresh)
			if err := json.Unmarshal([]byte(raw), &pair); err != nil {
				t.Fatal(err)
			}
			if pair.AccessTokenExpiresAt != accessExpiry || pair.RefreshTokenExpiresAt != refreshExpiry {
				t.Fatal("expiration digits changed")
			}
			if err := pair.Validate(time.Now()); err != nil {
				t.Fatal(err)
			}
			stored, _ := json.Marshal(pair)
			var readback SquarespaceOAuthTokenPair
			if err := json.Unmarshal(stored, &readback); err != nil || pair != readback {
				t.Fatal("canonical encrypted-store JSON roundtrip changed pair")
			}
		})
	}
	for _, test := range []struct {
		name, access, refresh string
		omitAccess            bool
	}{
		{"missing", "", refreshExpiry, true},
		{"null", "null", refreshExpiry, false},
		{"bool", "true", refreshExpiry, false},
		{"object", "{}", refreshExpiry, false},
		{"array", "[]", refreshExpiry, false},
		{"empty_string", `""`, refreshExpiry, false},
		{"oversized_string", strconv.Quote(strings.Repeat("1", 150)), refreshExpiry, false},
		{"oversized_number", strings.Repeat("1", 150), refreshExpiry, false},
		{"expired", `"1"`, refreshExpiry, false},
		{"negative", "-1", refreshExpiry, false},
		{"zero", "0", refreshExpiry, false},
		{"inf", `"+Inf"`, refreshExpiry, false},
		{"nan", `"NaN"`, refreshExpiry, false},
		{"overflow", "1e999", refreshExpiry, false},
		{"reversed_expirations", refreshExpiry, accessExpiry, false},
		{"expired_refresh", accessExpiry, `"1"`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fields := fmt.Sprintf(`,"access_token_expires_at":%s`, test.access)
			if test.omitAccess {
				fields = ""
			}
			raw := fmt.Sprintf(`{"access_token":"MOCK_ACCESS","refresh_token":"MOCK_REFRESH"%s,"refresh_token_expires_at":%s}`, fields, test.refresh)
			var pair SquarespaceOAuthTokenPair
			err := json.Unmarshal([]byte(raw), &pair)
			if err == nil {
				err = pair.Validate(time.Now())
			}
			if err == nil {
				t.Fatal("invalid expiration accepted")
			}
			if strings.Contains(err.Error(), "MOCK_") {
				t.Fatal("credential leaked in expiration diagnostics")
			}
		})
	}
}

func TestSquarespaceOAuthFailureCodeWhitelistNeverLeaksWrappedErrors(t *testing.T) {
	for _, test := range []struct {
		kind   string
		status int
		want   string
	}{
		{"oauth_request_outcome_unknown", 0, "OAUTH_REQUEST_OUTCOME_UNKNOWN"},
		{"invalid_oauth_response", 200, "OAUTH_RESPONSE_INVALID_HTTP_200"},
		{"invalid_oauth_expiration", 200, "OAUTH_EXPIRATION_INVALID_HTTP_200"},
		{"invalid_offline_oauth_pair", 200, "OAUTH_PAIR_INVALID_HTTP_200"},
		{"invalid_oauth_pair_expirations", 200, "OAUTH_EXPIRATIONS_INVALID_HTTP_200"},
		{"expired_oauth_pair", 200, "OAUTH_RESPONSE_EXPIRED_HTTP_200"},
		{"oauth_request_rejected", 401, "OAUTH_REQUEST_REJECTED_HTTP_401"},
		{"rate_limited", 429, "OAUTH_RATE_LIMITED_HTTP_429"},
		{"redirect_rejected", 302, "OAUTH_REDIRECT_REJECTED_HTTP_302"},
		{"invalid_refresh_token", 0, "OAUTH_REFRESH_TOKEN_INVALID"},
		{"invalid_oauth_request", 0, "OAUTH_REQUEST_INVALID"},
		{"MOCK_REFRESH_TOKEN", 200, "OAUTH_ROTATION_UNKNOWN"},
		{"invalid_oauth_response", 999, "OAUTH_RESPONSE_INVALID"},
	} {
		wrapped := fmt.Errorf("MOCK_CLIENT_SECRET wrapper: %w", &SquarespaceAPIError{Kind: test.kind, HTTPStatus: test.status})
		got := SquarespaceOAuthFailureCode(wrapped)
		if got != test.want || len(got) > 64 || strings.Contains(got, "MOCK_") {
			t.Errorf("classification=%q want %q", got, test.want)
		}
	}
	for _, err := range []error{nil, errors.New("MOCK_REFRESH_TOKEN"), fmt.Errorf("MOCK_CLIENT_SECRET: %w", errors.New("private@example.test")), (*SquarespaceAPIError)(nil)} {
		if got := SquarespaceOAuthFailureCode(err); got != "OAUTH_ROTATION_UNKNOWN" {
			t.Fatal("unknown error text escaped whitelist")
		}
	}
}

func TestSquarespaceOAuthMalformedSuccessResponseRetainsSafeHTTPClassification(t *testing.T) {
	client, _ := NewSquarespaceOAuthClient("CLIENT", "MOCK_CLIENT_SECRET")
	client.httpClient = &http.Client{Transport: squarespaceTestTransport(func(*http.Request) (*http.Response, error) {
		return squarespaceTestResponse(200, `{"access_token":"MOCK_ACCESS","refresh_token":"MOCK_REFRESH","access_token_expires_at":null,"refresh_token_expires_at":123}`), nil
	})}
	_, err := client.Refresh(context.Background(), "MOCK_OLD_REFRESH")
	if code := SquarespaceOAuthFailureCode(err); code != "OAUTH_EXPIRATION_INVALID_HTTP_200" {
		t.Fatalf("lost success-response parse diagnosis: %s", code)
	}
	if strings.Contains(err.Error(), "MOCK_") {
		t.Fatal("malformed token response leaked")
	}
}
