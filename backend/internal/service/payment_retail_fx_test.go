package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func retailFXTestManualConfig(now time.Time) *PaymentConfig {
	return &PaymentConfig{
		BalanceRetailFXSource:      "manual",
		BalanceRetailFXUSDPerGBP:   1.25,
		BalanceRetailFXCNYPerGBP:   8.75,
		BalanceRetailFXAsOf:        now.Add(-time.Hour).Format(time.RFC3339Nano),
		BalanceRetailFXMaxAgeHours: 120,
	}
}

func retailFXTestDocument(date, entries string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<gesmes:Envelope xmlns:gesmes="%s" xmlns="%s">
  <gesmes:subject>Reference rates</gesmes:subject>
  <gesmes:Sender><gesmes:name>European Central Bank</gesmes:name></gesmes:Sender>
  <Cube><Cube time="%s">%s</Cube></Cube>
</gesmes:Envelope>`, retailFXEnvelopeNS, retailFXECBNamespace, date, entries)
}

func retailFXTestValidDocument(date string) string {
	return retailFXTestDocument(date, `<Cube currency="USD" rate="1.25"/>
<Cube currency="GBP" rate="0.8"/>
<Cube currency="CNY" rate="8"/>
<Cube currency="JPY" rate="175.12"/>`)
}

func retailFXTestAssertUnavailable(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, http.StatusServiceUnavailable, infraerrors.Code(err))
	require.Equal(t, retailFXErrorReason, infraerrors.Reason(err))
}

func TestResolveRetailFXManual(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	cfg := retailFXTestManualConfig(now)
	cfg.BalanceRetailFXSource = " Manual "
	cfg.BalanceRetailFXAsOf = "2026-09-29T13:00:00+02:00"
	fx, err := ResolveRetailFX(context.Background(), cfg, now)
	require.NoError(t, err)
	require.Equal(t, "manual", fx.Source)
	require.Equal(t, map[string]float64{"GBP": 1, "USD": 1.25, "CNY": 8.75}, fx.Rates)
	require.Equal(t, now.Add(-time.Hour), fx.AsOf)
	require.Equal(t, time.UTC, fx.AsOf.Location())

	// Results are independent of the caller's configuration and other callers.
	fx.Rates["USD"] = 100
	next, err := ResolveRetailFX(context.Background(), cfg, now)
	require.NoError(t, err)
	require.Equal(t, 1.25, next.Rates["USD"])
}

func TestResolveRetailFXManualRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*PaymentConfig)
	}{
		{"empty source", func(c *PaymentConfig) { c.BalanceRetailFXSource = "" }},
		{"unknown source", func(c *PaymentConfig) { c.BalanceRetailFXSource = "https://example.com" }},
		{"missing timestamp", func(c *PaymentConfig) { c.BalanceRetailFXAsOf = "" }},
		{"invalid timestamp", func(c *PaymentConfig) { c.BalanceRetailFXAsOf = "2026-09-29" }},
		{"invalid timezone hour", func(c *PaymentConfig) { c.BalanceRetailFXAsOf = "2026-09-29T11:00:00+24:00" }},
		{"invalid timezone minute", func(c *PaymentConfig) { c.BalanceRetailFXAsOf = "2026-09-29T11:00:00+01:60" }},
		{"invalid fractional separator", func(c *PaymentConfig) { c.BalanceRetailFXAsOf = "2026-09-29T11:00:00,123Z" }},
		{"zero timestamp", func(c *PaymentConfig) { c.BalanceRetailFXAsOf = "0001-01-01T00:00:00Z" }},
		{"future timestamp", func(c *PaymentConfig) {
			c.BalanceRetailFXAsOf = now.Add(5*time.Minute + time.Nanosecond).Format(time.RFC3339Nano)
		}},
		{"stale timestamp", func(c *PaymentConfig) {
			c.BalanceRetailFXAsOf = now.Add(-120*time.Hour - time.Nanosecond).Format(time.RFC3339Nano)
		}},
		{"default age rejects stale", func(c *PaymentConfig) {
			c.BalanceRetailFXMaxAgeHours = 0
			c.BalanceRetailFXAsOf = now.Add(-121 * time.Hour).Format(time.RFC3339Nano)
		}},
		{"negative max age", func(c *PaymentConfig) { c.BalanceRetailFXMaxAgeHours = -1 }},
		{"excessive max age", func(c *PaymentConfig) { c.BalanceRetailFXMaxAgeHours = 241 }},
		{"USD zero", func(c *PaymentConfig) { c.BalanceRetailFXUSDPerGBP = 0 }},
		{"USD negative", func(c *PaymentConfig) { c.BalanceRetailFXUSDPerGBP = -1 }},
		{"USD NaN", func(c *PaymentConfig) { c.BalanceRetailFXUSDPerGBP = math.NaN() }},
		{"USD infinity", func(c *PaymentConfig) { c.BalanceRetailFXUSDPerGBP = math.Inf(1) }},
		{"CNY zero", func(c *PaymentConfig) { c.BalanceRetailFXCNYPerGBP = 0 }},
		{"CNY negative", func(c *PaymentConfig) { c.BalanceRetailFXCNYPerGBP = -1 }},
		{"CNY NaN", func(c *PaymentConfig) { c.BalanceRetailFXCNYPerGBP = math.NaN() }},
		{"CNY infinity", func(c *PaymentConfig) { c.BalanceRetailFXCNYPerGBP = math.Inf(-1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := retailFXTestManualConfig(now)
			tt.mutate(cfg)
			fx, err := ResolveRetailFX(context.Background(), cfg, now)
			require.Nil(t, fx)
			retailFXTestAssertUnavailable(t, err)
		})
	}

	fx, err := ResolveRetailFX(context.Background(), nil, now)
	require.Nil(t, fx)
	retailFXTestAssertUnavailable(t, err)
	fx, err = ResolveRetailFX(context.Background(), retailFXTestManualConfig(now), time.Time{})
	require.Nil(t, fx)
	retailFXTestAssertUnavailable(t, err)
	fx, err = ResolveRetailFX(nil, retailFXTestManualConfig(now), now)
	require.Nil(t, fx)
	retailFXTestAssertUnavailable(t, err)
}

func TestResolveRetailFXManualTimeBoundaries(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		age    time.Duration
		maxAge int
	}{
		{"default maximum age", 120 * time.Hour, 0},
		{"configured maximum age", 240 * time.Hour, 240},
		{"future tolerance", -5 * time.Minute, 120},
		{"one hour threshold", time.Hour, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := retailFXTestManualConfig(now)
			cfg.BalanceRetailFXAsOf = now.Add(-tt.age).Format(time.RFC3339Nano)
			cfg.BalanceRetailFXMaxAgeHours = tt.maxAge
			fx, err := ResolveRetailFX(context.Background(), cfg, now)
			require.NoError(t, err)
			require.Equal(t, now.Add(-tt.age), fx.AsOf)
		})
	}
}

func TestResolveRetailFXCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	fx, err := ResolveRetailFX(ctx, retailFXTestManualConfig(now), now)
	require.Nil(t, fx)
	retailFXTestAssertUnavailable(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestReadECBRetailFXCrossRates(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "application/xml, text/xml", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, retailFXTestValidDocument("2026-09-29"))
	}))
	defer server.Close()
	fx, err := readECBWithClient(context.Background(), server.Client(), server.URL)
	require.NoError(t, err)
	require.Equal(t, "ecb", fx.Source)
	require.Equal(t, map[string]float64{"GBP": 1, "USD": 1.5625, "CNY": 10}, fx.Rates)
	require.Equal(t, time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC), fx.AsOf)
}

func TestReadECBRetailFXRejectsHTTPFailures(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusNoContent, http.StatusMovedPermanently, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()
			fx, err := readECBWithClient(context.Background(), server.Client(), server.URL)
			require.Nil(t, fx)
			retailFXTestAssertUnavailable(t, err)
		})
	}

	t.Run("nil client", func(t *testing.T) {
		fx, err := readECBWithClient(context.Background(), nil, retailFXECBEndpoint)
		require.Nil(t, fx)
		retailFXTestAssertUnavailable(t, err)
	})
	t.Run("bad endpoint", func(t *testing.T) {
		fx, err := readECBWithClient(context.Background(), &http.Client{}, "://bad")
		require.Nil(t, fx)
		retailFXTestAssertUnavailable(t, err)
	})
}

func TestReadECBRetailFXRejectsLargeResponses(t *testing.T) {
	t.Parallel()
	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprintf("chunked=%t", chunked), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if chunked {
					w.(http.Flusher).Flush()
				} else {
					w.Header().Set("Content-Length", fmt.Sprint(retailFXMaxResponseSize+1))
				}
				_, _ = io.WriteString(w, strings.Repeat(" ", retailFXMaxResponseSize+1))
			}))
			defer server.Close()
			fx, err := readECBWithClient(context.Background(), server.Client(), server.URL)
			require.Nil(t, fx)
			retailFXTestAssertUnavailable(t, err)
			require.Contains(t, infraerrors.Message(err), "too large")
		})
	}
}

func TestReadECBRetailFXRejectsUnsafeOrMalformedXML(t *testing.T) {
	t.Parallel()
	valid := retailFXTestValidDocument("2026-09-29")
	tests := []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"malformed", valid[:len(valid)-1]},
		{"extra root", valid + valid},
		{"DTD", `<!DOCTYPE Envelope [<!ENTITY rate "1.25">]>` + valid},
		{"external DTD", `<!DOCTYPE Envelope SYSTEM "https://example.invalid/evil.dtd">` + valid},
		{"processing instruction", `<?stylesheet evil?>` + valid},
		{"unexpected entity", strings.Replace(valid, `rate="1.25"`, `rate="&unknown;"`, 1)},
		{"wrong root namespace", strings.Replace(valid, retailFXEnvelopeNS, "https://example.invalid", 1)},
		{"wrong rates namespace", strings.Replace(valid, retailFXECBNamespace, "https://example.invalid", 1)},
		{"missing GBP", strings.Replace(valid, `<Cube currency="GBP" rate="0.8"/>`, "", 1)},
		{"missing USD", strings.Replace(valid, `<Cube currency="USD" rate="1.25"/>`, "", 1)},
		{"missing CNY", strings.Replace(valid, `<Cube currency="CNY" rate="8"/>`, "", 1)},
		{"duplicate currency", strings.Replace(valid, `</Cube></Cube>`, `<Cube currency="USD" rate="1.3"/></Cube></Cube>`, 1)},
		{"duplicate currency attribute", strings.Replace(valid, `currency="USD"`, `currency="USD" currency="USD"`, 1)},
		{"invalid currency code", strings.Replace(valid, `currency="JPY"`, `currency="jpy"`, 1)},
		{"unknown attribute", strings.Replace(valid, `currency="USD"`, `currency="USD" other="x"`, 1)},
		{"invalid date", strings.Replace(valid, `time="2026-09-29"`, `time="2026-02-30"`, 1)},
		{"missing date", strings.Replace(valid, `time="2026-09-29"`, "", 1)},
		{"zero date", strings.Replace(valid, `time="2026-09-29"`, `time="0001-01-01"`, 1)},
		{"multiple dates", strings.Replace(valid, `</Cube></Cube>`, `</Cube><Cube time="2026-09-28"><Cube currency="USD" rate="1.25"/></Cube></Cube>`, 1)},
		{"unknown rates element", strings.Replace(valid, `<Cube currency="USD"`, `<Unknown/><Cube currency="USD"`, 1)},
		{"unexpected rates text", strings.Replace(valid, `<Cube currency="USD"`, `unexpected<Cube currency="USD"`, 1)},
		{"zero rate", strings.Replace(valid, `rate="1.25"`, `rate="0"`, 1)},
		{"negative rate", strings.Replace(valid, `rate="1.25"`, `rate="-1"`, 1)},
		{"NaN rate", strings.Replace(valid, `rate="1.25"`, `rate="NaN"`, 1)},
		{"infinite rate", strings.Replace(valid, `rate="1.25"`, `rate="+Inf"`, 1)},
		{"overflow rate", strings.Replace(valid, `rate="1.25"`, `rate="1e400"`, 1)},
		{"invalid ignored currency rate", strings.Replace(valid, `rate="175.12"`, `rate="NaN"`, 1)},
		{"cross rate overflow", strings.Replace(strings.Replace(valid, `rate="1.25"`, `rate="1e308"`, 1), `rate="0.8"`, `rate="1e-308"`, 1)},
		{"cross rate underflow", strings.Replace(strings.Replace(valid, `rate="1.25"`, `rate="1e-308"`, 1), `rate="0.8"`, `rate="1e308"`, 1)},
		{"excessive nesting", strings.Replace(valid, `<gesmes:subject>Reference rates</gesmes:subject>`, strings.Repeat("<deep>", 17)+strings.Repeat("</deep>", 17), 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			fx, err := readECBWithClient(context.Background(), server.Client(), server.URL)
			require.Nil(t, fx)
			retailFXTestAssertUnavailable(t, err)
		})
	}
}

type retailFXTestRoundTripper func(*http.Request) (*http.Response, error)

func (f retailFXTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReadECBRetailFXRequestDeadline(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: retailFXTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), retailFXHTTPTimeout)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	fx, err := readECBWithClient(ctx, client, retailFXECBEndpoint)
	require.Nil(t, fx)
	retailFXTestAssertUnavailable(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestResolveRetailFXECBRejectsStaleAndFuture(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for _, date := range []string{"2026-09-24", "2026-09-30"} {
		t.Run(date, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, retailFXTestValidDocument(date))
			}))
			defer server.Close()
			resolver := &retailFXResolver{client: server.Client(), endpoint: server.URL}
			fx, err := resolver.resolve(context.Background(), &PaymentConfig{BalanceRetailFXSource: "ecb"}, now)
			require.Nil(t, fx)
			retailFXTestAssertUnavailable(t, err)
			require.Nil(t, resolver.cached)
		})
	}
}

func TestResolveRetailFXECBCacheIsolationAndExpiry(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, retailFXTestValidDocument("2026-09-29"))
	}))
	defer server.Close()
	resolver := &retailFXResolver{client: server.Client(), endpoint: server.URL}
	cfg := &PaymentConfig{BalanceRetailFXSource: "ecb", BalanceRetailFXMaxAgeHours: 120}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	fx, err := resolver.resolve(context.Background(), cfg, now)
	require.NoError(t, err)
	fx.Rates["USD"] = 999
	cached, err := resolver.resolve(context.Background(), cfg, now.Add(59*time.Minute))
	require.NoError(t, err)
	require.Equal(t, 1.5625, cached.Rates["USD"])
	require.Equal(t, int32(1), calls.Load())
	cached.Rates["CNY"] = 999

	var wg sync.WaitGroup
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fx, err := resolver.resolve(context.Background(), cfg, now.Add(59*time.Minute))
			if err != nil {
				errors <- err
				return
			}
			if fx.Rates["CNY"] != 10 {
				errors <- fmt.Errorf("cached CNY rate = %v, want 10", fx.Rates["CNY"])
				return
			}
			fx.Rates["USD"] = 100
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), calls.Load())

	_, err = resolver.resolve(context.Background(), cfg, now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load())
	fail.Store(true)
	fx, err = resolver.resolve(context.Background(), cfg, now.Add(2*time.Hour))
	require.Nil(t, fx, "an expired cache cannot mask an upstream failure")
	retailFXTestAssertUnavailable(t, err)
	require.Equal(t, int32(3), calls.Load())
}

func TestResolveRetailFXECBCacheRevalidatesSourceAge(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, retailFXTestValidDocument("2026-09-29"))
	}))
	defer server.Close()
	resolver := &retailFXResolver{client: server.Client(), endpoint: server.URL}
	cfg := &PaymentConfig{BalanceRetailFXSource: "ecb", BalanceRetailFXMaxAgeHours: 2}
	now := time.Date(2026, 9, 29, 1, 30, 0, 0, time.UTC)
	_, err := resolver.resolve(context.Background(), cfg, now)
	require.NoError(t, err)
	fx, err := resolver.resolve(context.Background(), cfg, now.Add(31*time.Minute))
	require.Nil(t, fx)
	retailFXTestAssertUnavailable(t, err)
	require.Equal(t, int32(2), calls.Load(), "cache age under one hour must still revalidate the source date")

	cfg.BalanceRetailFXMaxAgeHours = 1
	fx, err = resolver.resolve(context.Background(), cfg, now)
	require.Nil(t, fx)
	retailFXTestAssertUnavailable(t, err)
	require.Equal(t, int32(3), calls.Load(), "tightened settings must invalidate cached rates")
}

func TestResolveRetailFXProductionHTTPPolicy(t *testing.T) {
	t.Parallel()
	require.Equal(t, retailFXECBEndpoint, defaultRetailFXResolver.endpoint)
	require.Equal(t, retailFXHTTPTimeout, defaultRetailFXResolver.client.Timeout)
	require.ErrorIs(t, defaultRetailFXResolver.client.CheckRedirect(nil, nil), http.ErrUseLastResponse)
}
