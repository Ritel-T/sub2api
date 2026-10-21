package provider

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func TestSquarespacePauseStopsTokenAndHTTPDispatch(t *testing.T) {
	t.Setenv("SQUARESPACE_PAYMENTS_PAUSED", "true")
	ctx := context.Background()
	client, err := NewSquarespaceClient("site_1", SquarespaceReferenceFieldLabel, SquarespaceTokenSourceFunc(func(context.Context) (string, error) { t.Fatal("token source called"); return "", nil }))
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient = &http.Client{Transport: squarespaceTestTransport(func(*http.Request) (*http.Response, error) { t.Fatal("network dispatched"); return nil, nil })}
	check := func(err error) {
		t.Helper()
		if !errors.Is(err, payment.ErrSquarespacePaymentsPaused) {
			t.Fatalf("unexpected pause error: %v", err)
		}
	}
	check(client.VerifyWebsite(ctx))
	check(client.fetchJSON(ctx, "MOCK_TOKEN", "/forbidden", nil, nil))
	_, err = client.GetOrder(ctx, "order_1")
	check(err)
	oauth, err := NewSquarespaceOAuthClient("MOCK_CLIENT", "MOCK_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	oauth.httpClient = client.httpClient
	_, err = oauth.Refresh(ctx, "MOCK_REFRESH")
	check(err)
	_, err = oauth.RedeemCode(ctx, "MOCK_CODE", "http://localhost:8090/oauth/callback")
	check(err)
	_, err = oauth.request(ctx, url.Values{})
	check(err)
	_, err = SquarespaceOfflineAuthorizeURL("MOCK_CLIENT", "http://localhost:8090/oauth/callback", "MOCK_STATE")
	check(err)
	p := &Squarespace{}
	_, err = p.CreatePayment(ctx, payment.CreatePaymentRequest{})
	check(err)
}
