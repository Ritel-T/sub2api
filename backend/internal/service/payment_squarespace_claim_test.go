//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type squarespaceClaimRedisTestCache struct {
	client *redis.Client
	failed atomic.Bool
}

func (c *squarespaceClaimRedisTestCache) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	if c.failed.Load() {
		return nil, errors.New("claim Redis unavailable")
	}
	return c.client.Eval(ctx, script, keys, args...).Result()
}
func (c *squarespaceClaimRedisTestCache) Get(ctx context.Context, key string) (string, error) {
	if c.failed.Load() {
		return "", errors.New("claim Redis unavailable")
	}
	value, err := c.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrSquarespaceClaimCacheMiss
	}
	return value, err
}
func (c *squarespaceClaimRedisTestCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if c.failed.Load() {
		return errors.New("claim Redis unavailable")
	}
	return c.client.Set(ctx, key, value, ttl).Err()
}
func (c *squarespaceClaimRedisTestCache) Del(ctx context.Context, key string) error {
	if c.failed.Load() {
		return errors.New("claim Redis unavailable")
	}
	return c.client.Del(ctx, key).Err()
}

type squarespaceClaimTestFixture struct {
	service    *SquarespaceClaimService
	redis      *miniredis.Miniredis
	cache      *squarespaceClaimRedisTestCache
	now        time.Time
	user       *User
	local      *dbent.PaymentOrder
	remote     *provider.SquarespaceOrder
	docs       []provider.SquarespaceTransactionDocument
	code       string
	sendErr    error
	lookupErr  error
	unboundErr error
	bindErr    error
	mu         sync.Mutex
	recipients []string
	bodies     []string
	binds      atomic.Int64
	lookups    atomic.Int64
}

func newSquarespaceClaimTestFixture(t *testing.T) *squarespaceClaimTestFixture {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	f := &squarespaceClaimTestFixture{
		redis: mr,
		cache: &squarespaceClaimRedisTestCache{client: rdb},
		now:   time.Now().UTC().Truncate(time.Second),
		user:  &User{ID: 7, Email: "p.ayer+receipt@gmail.com", Status: StatusActive},
		code:  "729134",
	}
	f.remote, f.docs = squarespaceBridgeProofFixture()
	f.remote.CustomerEmail = f.user.Email
	f.remote.TopUpReference = ""
	f.remote.ReferenceStatus = "missing"
	f.docs[0].Payments[0].PaidOn = f.now.Add(-time.Minute).Format(time.RFC3339Nano)
	quote := &RetailQuote{
		CreditedAmountUSD: 20, BaseAmountGBP: 9.5, IncludedCostGBP: .5,
		TotalAmountGBP: 10, PayAmount: 10, Currency: "GBP",
		FX: map[string]float64{"GBP": 1, "USD": 2, "CNY": 10}, FXSource: "manual", FXAsOf: f.now,
		IssuedAt: f.now.Add(-2 * time.Minute), ExpiresAt: f.now.Add(2 * time.Minute),
		CheckoutReference: "sub2_0123456789abcdef0123456789abcdef", PaymentClaimMode: "receipt_otp",
		ProductID: f.remote.LineItems[0].ProductID,
	}
	key, instance := "squarespace", "3"
	f.local = &dbent.PaymentOrder{
		ID: 101, UserID: f.user.ID, UserEmail: f.user.Email,
		Amount: 20, PayAmount: 10, OutTradeNo: quote.CheckoutReference,
		PaymentType: key, ProviderKey: &key, ProviderInstanceID: &instance,
		OrderType: "balance", Status: OrderStatusPending, ExpiresAt: quote.ExpiresAt,
		ProviderSnapshot: map[string]any{
			"schema_version": 1, "provider_instance_id": instance, "provider_key": key,
			"merchant_id": "site_1", "currency": "GBP", "product_id": quote.ProductID, "retail_quote": retailQuoteSnapshot(quote),
		},
	}
	require.NoError(t, ValidateSquarespaceReceiptClaimProof(f.local, f.remote, f.docs), "the fixture must carry genuine financial proof")
	f.service = NewSquarespaceClaimService(nil, nil, f.cache)
	f.service.now = func() time.Time { return f.now }
	f.service.validateAccess = func(context.Context, int64, *dbent.PaymentOrder) error { return nil }
	f.service.signingKey = func() ([]byte, error) { return []byte("0123456789abcdef0123456789abcdef"), nil }
	f.service.loadUser = func(_ context.Context, uid int64) (*User, error) {
		if uid != f.user.ID {
			return nil, ErrUserNotFound
		}
		user := *f.user
		return &user, nil
	}
	f.service.loadOrder = func(_ context.Context, id, uid int64) (*dbent.PaymentOrder, error) {
		if id != f.local.ID || uid != f.local.UserID {
			return nil, errors.New("order does not belong to user")
		}
		encoded, err := json.Marshal(f.local)
		if err != nil {
			return nil, err
		}
		var local dbent.PaymentOrder
		if err := json.Unmarshal(encoded, &local); err != nil {
			return nil, err
		}
		return &local, nil
	}
	f.service.lookupReceipt = func(_ context.Context, _ *dbent.PaymentOrder, receipt string) (*provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument, error) {
		f.lookups.Add(1)
		if f.lookupErr != nil {
			return nil, nil, f.lookupErr
		}
		number, err := normalizeSquarespaceReceiptNumber(receipt)
		if err != nil || number != f.remote.OrderNumber {
			return nil, nil, errors.New("receipt identity unavailable")
		}
		remote := *f.remote
		encoded, err := json.Marshal(f.docs)
		if err != nil {
			return nil, nil, err
		}
		var docs []provider.SquarespaceTransactionDocument
		if err := json.Unmarshal(encoded, &docs); err != nil {
			return nil, nil, err
		}
		return &remote, docs, nil
	}
	f.service.ensureUnbound = func(context.Context, *dbent.PaymentOrder, *provider.SquarespaceOrder) error { return f.unboundErr }
	f.service.bind = func(ctx context.Context, local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, _ []provider.SquarespaceTransactionDocument) error {
		authority, ok := ctx.Value(squarespaceVerifiedReceiptClaimKey{}).(squarespaceVerifiedReceiptClaimAuthority)
		hash, err := SquarespaceReceiptQuoteHash(local)
		if err != nil || !ok || authority.userID != local.UserID || authority.localOrderID != local.ID || authority.externalOrderID != remote.ID || authority.quoteHash != hash {
			return errors.New("missing or mismatched verified claim authority")
		}
		f.binds.Add(1)
		return f.bindErr
	}
	f.service.sendEmail = func(_ context.Context, to, _ string, body string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.recipients = append(f.recipients, to)
		f.bodies = append(f.bodies, body)
		return f.sendErr
	}
	f.service.generateCode = func() (string, error) { return f.code, nil }
	return f
}

func (f *squarespaceClaimTestFixture) advance(d time.Duration) {
	f.now = f.now.Add(d)
	f.redis.FastForward(d)
}
func (f *squarespaceClaimTestFixture) updateQuote(change func(*RetailQuote)) {
	quote := PaymentOrderRetailQuote(f.local)
	change(quote)
	f.local.ProviderSnapshot["retail_quote"] = retailQuoteSnapshot(quote)
}
func requireSquarespaceClaimReason(t *testing.T, err error, reasons ...string) {
	t.Helper()
	require.Error(t, err)
	require.Contains(t, reasons, infraerrors.Reason(err), "unexpected claim error reason")
}
func (f *squarespaceClaimTestFixture) request(t *testing.T) string {
	t.Helper()
	challenge, err := f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "#1")
	require.NoError(t, err)
	require.NotNil(t, challenge)
	require.NotEmpty(t, challenge.ChallengeToken)
	require.Equal(t, 60, challenge.ResendAfterSeconds)
	require.WithinDuration(t, f.now.Add(10*time.Minute), challenge.ExpiresAt, time.Second)
	return challenge.ChallengeToken
}

func TestSquarespaceClaimFreshMailboxProofAndPrivateCache(t *testing.T) {
	f := newSquarespaceClaimTestFixture(t)
	token := f.request(t)
	require.Equal(t, []string{f.remote.CustomerEmail}, f.recipients)
	require.Contains(t, f.bodies[0], f.code)
	var records int
	for _, key := range f.redis.Keys() {
		value, err := f.redis.Get(key)
		require.NoError(t, err)
		require.NotContains(t, key, f.remote.CustomerEmail)
		require.NotContains(t, value, f.remote.CustomerEmail)
		require.NotContains(t, value, token)
		require.NotContains(t, value, `"`+f.code+`"`)
		var record map[string]any
		if json.Unmarshal([]byte(value), &record) == nil && len(record) > 0 {
			records++
		}
	}
	require.Positive(t, records, "challenge binding must be present before claim")
	order, err := f.service.Claim(context.Background(), f.user.ID, token, f.code)
	require.NoError(t, err)
	require.NotNil(t, order)
	require.Equal(t, f.local.ID, order.ID)
	require.Equal(t, int64(1), f.binds.Load())
	require.GreaterOrEqual(t, f.lookups.Load(), int64(2), "claim must re-read trusted payment proof")
	_, err = f.service.Claim(context.Background(), f.user.ID, token, f.code)
	requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID", "SQUARESPACE_CLAIM_UNAVAILABLE")
	require.Equal(t, int64(1), f.binds.Load())
}

func TestSquarespaceClaimStrictBareMailboxIdentity(t *testing.T) {
	cases := []struct {
		name, account, payer string
		accepted             bool
	}{
		{"case and whitespace", " P.Ayer+Receipt@GMAIL.COM ", "p.ayer+receipt@gmail.com", true},
		{"plus suffix stays significant", "p.ayer@gmail.com", "p.ayer+receipt@gmail.com", false},
		{"Gmail dots stay significant", "payer+receipt@gmail.com", "p.ayer+receipt@gmail.com", false},
		{"Gmail domain aliases stay significant", "p.ayer+receipt@googlemail.com", "p.ayer+receipt@gmail.com", false},
		{"account display name rejected", "Buyer <p.ayer+receipt@gmail.com>", "p.ayer+receipt@gmail.com", false},
		{"payer display name rejected", "p.ayer+receipt@gmail.com", "Buyer <p.ayer+receipt@gmail.com>", false},
		{"multiple mailboxes rejected", "p.ayer+receipt@gmail.com", "p.ayer+receipt@gmail.com, other@gmail.com", false},
		{"missing upstream mailbox", "p.ayer+receipt@gmail.com", "", false},
		{"synthetic mailbox", "wechat-7@wechat-connect.invalid", "wechat-7@wechat-connect.invalid", false},
		{"other account mailbox", "other@gmail.com", "p.ayer+receipt@gmail.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSquarespaceClaimTestFixture(t)
			f.user.Email, f.remote.CustomerEmail = tc.account, tc.payer
			challenge, err := f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "1")
			if tc.accepted {
				require.NoError(t, err)
				require.NotNil(t, challenge)
				require.Equal(t, []string{tc.payer}, f.recipients)
			} else {
				requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_UNAVAILABLE")
				require.Empty(t, f.recipients)
				require.Zero(t, f.binds.Load())
			}
		})
	}
}

func TestSquarespaceClaimRefusesUnownedOrUnavailableOrders(t *testing.T) {
	cases := map[string]func(*squarespaceClaimTestFixture){
		"order belongs to another UID": func(f *squarespaceClaimTestFixture) { f.local.UserID++ },
		"inactive user":                func(f *squarespaceClaimTestFixture) { f.user.Status = "disabled" },
		"cancelled local order":        func(f *squarespaceClaimTestFixture) { f.local.Status = OrderStatusCancelled },
		"completed local order":        func(f *squarespaceClaimTestFixture) { f.local.Status = OrderStatusCompleted },
		"reference-only quote": func(f *squarespaceClaimTestFixture) {
			f.updateQuote(func(q *RetailQuote) { q.PaymentClaimMode = "reference" })
		},
		"receipt already bound": func(f *squarespaceClaimTestFixture) { f.unboundErr = ErrSquarespaceClaimUnavailable },
		"upstream unavailable":  func(f *squarespaceClaimTestFixture) { f.lookupErr = errors.New("official API down") },
		"missing quote":         func(f *squarespaceClaimTestFixture) { delete(f.local.ProviderSnapshot, "retail_quote") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSquarespaceClaimTestFixture(t)
			change(f)
			_, err := f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "1")
			requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_UNAVAILABLE", "SQUARESPACE_CLAIM_SERVICE_UNAVAILABLE")
			require.Empty(t, f.recipients)
			require.Zero(t, f.binds.Load())
		})
	}
}

func TestSquarespaceClaimRechecksFrozenIdentityAndFinancialProof(t *testing.T) {
	cases := map[string]func(*squarespaceClaimTestFixture){
		"current user email changed":   func(f *squarespaceClaimTestFixture) { f.user.Email = "new-owner@gmail.com" },
		"official payer email changed": func(f *squarespaceClaimTestFixture) { f.remote.CustomerEmail = "new-owner@gmail.com" },
		"both emails changed": func(f *squarespaceClaimTestFixture) {
			f.user.Email = "new-owner@gmail.com"
			f.remote.CustomerEmail = f.user.Email
		},
		"quote changed":   func(f *squarespaceClaimTestFixture) { f.updateQuote(func(q *RetailQuote) { q.FXSource = "other" }) },
		"website changed": func(f *squarespaceClaimTestFixture) { f.local.ProviderSnapshot["merchant_id"] = "site_other" },
		"owner changed":   func(f *squarespaceClaimTestFixture) { f.local.UserID++ },
		"external order changed": func(f *squarespaceClaimTestFixture) {
			f.remote.ID = "external_order_2"
			f.docs[0].SalesOrderID = f.remote.ID
		},
		"receipt number changed": func(f *squarespaceClaimTestFixture) { f.remote.OrderNumber = "2" },
		"currency changed":       func(f *squarespaceClaimTestFixture) { f.remote.GrandTotal.Currency = "USD" },
		"partial refund": func(f *squarespaceClaimTestFixture) {
			money := func(value string) provider.SquarespaceMoney {
				return provider.SquarespaceMoney{Currency: "GBP", Value: value}
			}
			f.remote.RefundedTotal = money("1.00")
			f.docs[0].TotalNetPayment = money("8.55")
			f.docs[0].Payments[0].RefundedAmount = money("1.00")
			f.docs[0].Payments[0].NetAmount = money("8.55")
			f.docs[0].Payments[0].Refunds = []provider.SquarespaceRefund{{ID: "refund_1", RefundedOn: f.now.Format(time.RFC3339Nano), Amount: money("1.00")}}
		},
		"refund pending": func(f *squarespaceClaimTestFixture) { f.remote.PaymentState = "REFUND_PENDING" },
		"old paidOn outside quote": func(f *squarespaceClaimTestFixture) {
			f.docs[0].Payments[0].PaidOn = f.now.Add(-time.Hour).Format(time.RFC3339Nano)
		},
		"already uniquely bound": func(f *squarespaceClaimTestFixture) { f.unboundErr = ErrSquarespaceClaimUnavailable },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSquarespaceClaimTestFixture(t)
			token := f.request(t)
			change(f)
			_, err := f.service.Claim(context.Background(), f.user.ID, token, f.code)
			requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_UNAVAILABLE", "SQUARESPACE_CLAIM_CODE_INVALID")
			require.Zero(t, f.binds.Load())
		})
	}
}

func TestSquarespaceClaimExpiredLocalOrderHonorsOfficialPaidTime(t *testing.T) {
	f := newSquarespaceClaimTestFixture(t)
	f.local.Status = OrderStatusExpired
	f.updateQuote(func(q *RetailQuote) { q.IssuedAt = f.now.Add(-3 * time.Minute); q.ExpiresAt = f.now.Add(-time.Minute) })
	f.local.ExpiresAt = f.now.Add(-time.Minute)
	f.docs[0].Payments[0].PaidOn = f.now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	token := f.request(t)
	_, err := f.service.Claim(context.Background(), f.user.ID, token, f.code)
	require.NoError(t, err)
	require.Equal(t, int64(1), f.binds.Load())
}

func TestSquarespaceClaimFailedAttemptsAreAtomicAndExhausted(t *testing.T) {
	f := newSquarespaceClaimTestFixture(t)
	token := f.request(t)
	start := make(chan struct{})
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.service.Claim(context.Background(), f.user.ID, token, "000000")
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID", "SQUARESPACE_CLAIM_RATE_LIMITED")
	}
	_, err := f.service.Claim(context.Background(), f.user.ID, token, f.code)
	requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID", "SQUARESPACE_CLAIM_RATE_LIMITED")
	require.Zero(t, f.binds.Load())
}

func TestSquarespaceClaimCorrectCodeConsumedExactlyOnce(t *testing.T) {
	f := newSquarespaceClaimTestFixture(t)
	token := f.request(t)
	start := make(chan struct{})
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.service.Claim(context.Background(), f.user.ID, token, f.code)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID", "SQUARESPACE_CLAIM_UNAVAILABLE", "SQUARESPACE_CLAIM_RATE_LIMITED")
	}
	require.Equal(t, 1, successes)
	require.Equal(t, int64(1), f.binds.Load())
}

func TestSquarespaceClaimResendRevokesOldChallenge(t *testing.T) {
	f := newSquarespaceClaimTestFixture(t)
	old := f.request(t)
	_, err := f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "1")
	requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_RATE_LIMITED")
	require.Len(t, f.recipients, 1)
	f.advance(61 * time.Second)
	f.code = "318527"
	current := f.request(t)
	require.NotEqual(t, old, current)
	require.Len(t, f.recipients, 2)
	_, err = f.service.Claim(context.Background(), f.user.ID, old, "729134")
	requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID", "SQUARESPACE_CLAIM_UNAVAILABLE")
	require.Zero(t, f.binds.Load())
	_, err = f.service.Claim(context.Background(), f.user.ID, current, f.code)
	require.NoError(t, err)
	require.Equal(t, int64(1), f.binds.Load())
}

func TestSquarespaceClaimExpiryTamperingAndOtherUID(t *testing.T) {
	for _, kind := range []string{"expiry", "tampering", "other UID"} {
		t.Run(kind, func(t *testing.T) {
			f := newSquarespaceClaimTestFixture(t)
			token := f.request(t)
			uid := f.user.ID
			switch kind {
			case "expiry":
				f.advance(10*time.Minute + time.Second)
			case "tampering":
				replacement := "A"
				if strings.HasPrefix(token, replacement) {
					replacement = "B"
				}
				token = replacement + token[1:]
			case "other UID":
				uid++
			}
			_, err := f.service.Claim(context.Background(), uid, token, f.code)
			requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID", "SQUARESPACE_CLAIM_UNAVAILABLE")
			require.Zero(t, f.binds.Load())
		})
	}
}

func TestSquarespaceClaimSMTPFailureCleansChallenge(t *testing.T) {
	f := newSquarespaceClaimTestFixture(t)
	f.sendErr = errors.New("SMTP rejected test delivery")
	_, err := f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "1")
	requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_SERVICE_UNAVAILABLE", "SQUARESPACE_CLAIM_UNAVAILABLE")
	require.Zero(t, f.binds.Load())
	for _, key := range f.redis.Keys() {
		value, readErr := f.redis.Get(key)
		require.NoError(t, readErr)
		var record map[string]any
		if json.Unmarshal([]byte(value), &record) == nil {
			require.Empty(t, record, "failed delivery must not leave a usable challenge")
		}
	}
}

func TestSquarespaceClaimRedisOutageFailsClosed(t *testing.T) {
	t.Run("request", func(t *testing.T) {
		f := newSquarespaceClaimTestFixture(t)
		f.cache.failed.Store(true)
		_, err := f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "1")
		requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_SERVICE_UNAVAILABLE")
		require.Empty(t, f.recipients)
		require.Zero(t, f.binds.Load())
	})
	t.Run("submit", func(t *testing.T) {
		f := newSquarespaceClaimTestFixture(t)
		token := f.request(t)
		f.cache.failed.Store(true)
		_, err := f.service.Claim(context.Background(), f.user.ID, token, f.code)
		requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_SERVICE_UNAVAILABLE")
		require.Zero(t, f.binds.Load())
	})
}

func TestSquarespaceClaimPurposeAndCodeAuthorityCannotBeReused(t *testing.T) {
	for _, change := range []string{"purpose", "recipient MAC as OTP", "different challenge token"} {
		t.Run(change, func(t *testing.T) {
			f := newSquarespaceClaimTestFixture(t)
			token := f.request(t)
			signingKey, err := f.service.signingKey()
			require.NoError(t, err)
			cacheKey := squarespaceClaimTokenKey(signingKey, token)
			value, err := f.cache.Get(context.Background(), cacheKey)
			require.NoError(t, err)
			var data squarespaceClaimChallenge
			require.NoError(t, json.Unmarshal([]byte(value), &data))
			switch change {
			case "purpose":
				data.Purpose = "sub2api-retail-quote-v1"
			case "recipient MAC as OTP":
				data.CodeMAC = data.RecipientHash
			case "different challenge token":
				f.advance(61 * time.Second)
				token = f.request(t)
				cacheKey = squarespaceClaimTokenKey(signingKey, token)
			}
			encoded, err := json.Marshal(data)
			require.NoError(t, err)
			require.NoError(t, f.cache.Set(context.Background(), cacheKey, string(encoded), 10*time.Minute))
			_, err = f.service.Claim(context.Background(), f.user.ID, token, f.code)
			requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID")
			require.Zero(t, f.binds.Load())
		})
	}
}

func TestSquarespaceClaimRateWindowsAreBounded(t *testing.T) {
	t.Run("ten requests per UID per hour including failed receipts", func(t *testing.T) {
		f := newSquarespaceClaimTestFixture(t)
		f.lookupErr = errors.New("receipt unavailable")
		for range 10 {
			_, err := f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "1")
			requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_UNAVAILABLE")
		}
		_, err := f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "1")
		requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_RATE_LIMITED")
		require.Empty(t, f.recipients)
		f.advance(time.Hour + time.Second)
		_, err = f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "1")
		requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_UNAVAILABLE")
	})
	t.Run("thirty submits per UID per ten minutes including malformed tokens", func(t *testing.T) {
		f := newSquarespaceClaimTestFixture(t)
		for range 30 {
			_, err := f.service.Claim(context.Background(), f.user.ID, "malformed", f.code)
			requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID")
		}
		_, err := f.service.Claim(context.Background(), f.user.ID, "malformed", f.code)
		requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_RATE_LIMITED")
		require.Zero(t, f.binds.Load())
		f.advance(10*time.Minute + time.Second)
		_, err = f.service.Claim(context.Background(), f.user.ID, "malformed", f.code)
		requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID")
	})
}

func TestSquarespaceClaimFailedBindingCannotReplayOTP(t *testing.T) {
	f := newSquarespaceClaimTestFixture(t)
	token := f.request(t)
	f.bindErr = errors.New("binding failed after verified authority")
	_, err := f.service.Claim(context.Background(), f.user.ID, token, f.code)
	requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_UNAVAILABLE")
	require.Equal(t, int64(1), f.binds.Load())
	f.bindErr = nil
	_, err = f.service.Claim(context.Background(), f.user.ID, token, f.code)
	requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID")
	require.Equal(t, int64(1), f.binds.Load())
}

func TestSquarespaceClaimRechecksAccountAfterOfficialLookup(t *testing.T) {
	for _, phase := range []string{"issue", "submit"} {
		t.Run(phase, func(t *testing.T) {
			f := newSquarespaceClaimTestFixture(t)
			token := ""
			if phase == "submit" {
				token = f.request(t)
			}
			lookup := f.service.lookupReceipt
			f.service.lookupReceipt = func(ctx context.Context, local *dbent.PaymentOrder, number string) (*provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument, error) {
				remote, docs, err := lookup(ctx, local, number)
				f.user.Email = "changed-during-upstream-fetch@gmail.com"
				return remote, docs, err
			}
			var err error
			if phase == "issue" {
				_, err = f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "1")
				require.Empty(t, f.recipients)
			} else {
				_, err = f.service.Claim(context.Background(), f.user.ID, token, f.code)
				require.Len(t, f.recipients, 1)
			}
			requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_UNAVAILABLE")
			require.Zero(t, f.binds.Load())
		})
	}
}

func TestSquarespaceClaimDeniedChannelDoesNotQueryOrSend(t *testing.T) {
	f := newSquarespaceClaimTestFixture(t)
	f.service.validateAccess = func(context.Context, int64, *dbent.PaymentOrder) error { return ErrSquarespaceClaimUnavailable }
	_, err := f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "1")
	requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_UNAVAILABLE")
	require.Zero(t, f.lookups.Load())
	require.Empty(t, f.recipients)
	require.Zero(t, f.binds.Load())
}

func TestSquarespaceClaimAttemptBoundary(t *testing.T) {
	for _, wrongAttempts := range []int{4, 5} {
		t.Run(string(rune('0'+wrongAttempts))+" failed attempts", func(t *testing.T) {
			f := newSquarespaceClaimTestFixture(t)
			token := f.request(t)
			for range wrongAttempts {
				_, err := f.service.Claim(context.Background(), f.user.ID, token, "000000")
				requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID")
			}
			_, err := f.service.Claim(context.Background(), f.user.ID, token, f.code)
			if wrongAttempts == 4 {
				require.NoError(t, err)
				require.Equal(t, int64(1), f.binds.Load())
			} else {
				requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_CODE_INVALID")
				require.Zero(t, f.binds.Load())
			}
		})
	}
}

func TestSquarespaceClaimRechecksOrderAndAccessAfterOfficialLookup(t *testing.T) {
	for _, phase := range []string{"issue", "submit"} {
		for _, change := range []string{"cancelled order", "same-money quote mutation", "access revoked"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				f := newSquarespaceClaimTestFixture(t)
				token := ""
				challengeKey := ""
				initialChallenge := ""
				if phase == "submit" {
					token = f.request(t)
					key, err := f.service.signingKey()
					require.NoError(t, err)
					challengeKey = squarespaceClaimTokenKey(key, token)
					initialChallenge, err = f.cache.Get(context.Background(), challengeKey)
					require.NoError(t, err)
				}
				originalStatus := f.local.Status
				originalQuote := PaymentOrderRetailQuote(f.local)
				accessRevoked := false
				f.service.validateAccess = func(context.Context, int64, *dbent.PaymentOrder) error {
					if accessRevoked {
						return ErrSquarespaceClaimUnavailable
					}
					return nil
				}
				lookup := f.service.lookupReceipt
				changed := false
				f.service.lookupReceipt = func(ctx context.Context, local *dbent.PaymentOrder, number string) (*provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument, error) {
					remote, docs, err := lookup(ctx, local, number)
					if err == nil && !changed {
						changed = true
						switch change {
						case "cancelled order":
							f.local.Status = OrderStatusCancelled
						case "same-money quote mutation":
							f.updateQuote(func(q *RetailQuote) { q.FXAsOf = q.FXAsOf.Add(-time.Second) })
						case "access revoked":
							accessRevoked = true
						}
					}
					return remote, docs, err
				}
				var err error
				if phase == "issue" {
					_, err = f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, "1")
					require.Empty(t, f.recipients, "a stale local order or revoked channel must not issue OTP")
				} else {
					_, err = f.service.Claim(context.Background(), f.user.ID, token, f.code)
					require.Len(t, f.recipients, 1, "refusal must not send another OTP")
				}
				requireSquarespaceClaimReason(t, err, "SQUARESPACE_CLAIM_UNAVAILABLE")
				require.True(t, changed, "mutation must happen inside trusted upstream lookup")
				require.Zero(t, f.binds.Load(), "refusal must precede verified binding authority")
				if phase == "submit" {
					currentChallenge, readErr := f.cache.Get(context.Background(), challengeKey)
					require.NoError(t, readErr)
					require.Equal(t, initialChallenge, currentChallenge, "a rejected fresh-proof precheck must leave valid OTP untouched")
					f.local.Status = originalStatus
					f.local.ProviderSnapshot["retail_quote"] = retailQuoteSnapshot(originalQuote)
					accessRevoked = false
					_, err = f.service.Claim(context.Background(), f.user.ID, token, f.code)
					require.NoError(t, err, "the same OTP remains usable after authoritative state is restored")
					require.Equal(t, int64(1), f.binds.Load())
				}
			})
		}
	}
}

func TestSquarespaceClaimRejectsWrongGoodsEvenWithExactMailboxAndMoney(t *testing.T) {
	for _, kind := range []string{"different-product", "missing-product", "mixed-cart"} {
		t.Run(kind, func(t *testing.T) {
			f := newSquarespaceClaimTestFixture(t)
			switch kind {
			case "different-product":
				f.remote.LineItems[0].ProductID = "abcdef0123456789abcdef01"
			case "missing-product":
				f.remote.LineItems = nil
			case "mixed-cart":
				f.remote.LineItems = append(f.remote.LineItems, f.remote.LineItems[0])
			}
			_, err := f.service.RequestChallenge(context.Background(), f.user.ID, f.local.ID, f.remote.OrderNumber)
			require.ErrorIs(t, err, ErrSquarespaceClaimUnavailable)
			require.Empty(t, f.recipients)
			require.Equal(t, int64(0), f.binds.Load())
		})
	}
}
