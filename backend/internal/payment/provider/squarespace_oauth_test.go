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
