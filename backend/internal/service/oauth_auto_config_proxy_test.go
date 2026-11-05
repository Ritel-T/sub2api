//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type oauthDedicatedProxyRepoStub struct {
	ProxyRepository
	items  []ProxyWithAccountCount
	locked bool
	pages  []int
}

func (r *oauthDedicatedProxyRepoStub) LockAccountProxySelection(ctx context.Context) error {
	if dbent.TxFromContext(ctx) == nil {
		return errors.New("missing transaction")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.locked = true
	return nil
}

func (r *oauthDedicatedProxyRepoStub) ListAccountProxySelection(_ context.Context, p pagination.PaginationParams) ([]ProxyWithAccountCount, *pagination.PaginationResult, error) {
	if !r.locked {
		panic("listing before lock")
	}
	r.pages = append(r.pages, p.Page)
	from := min(p.Offset(), len(r.items))
	to := min(from+p.Limit(), len(r.items))
	return r.items[from:to], &pagination.PaginationResult{Total: int64(len(r.items))}, nil
}

type oauthDedicatedProberStub struct {
	mu      sync.Mutex
	results map[string]*ProxyExitInfo
	errors  map[string]error
	calls   []string
}

func (p *oauthDedicatedProberStub) ProbeProxy(_ context.Context, raw string) (*ProxyExitInfo, int64, error) {
	u, _ := url.Parse(raw)
	host := u.Hostname()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, host)
	return p.results[host], 1, p.errors[host]
}

func oauthProxyFixture(id int64, count int64) ProxyWithAccountCount {
	return ProxyWithAccountCount{Proxy: Proxy{ID: id, Host: fmt.Sprintf("proxy-%d.test", id), Protocol: "http", Port: 8080, Status: StatusActive}, AccountCount: count, CountryCode: "US"}
}
func oauthExitFixture(ip string) *ProxyExitInfo {
	return &ProxyExitInfo{IP: ip, CountryCode: "US", Timezone: "America/Los_Angeles"}
}
func oauthProxyTestContext() context.Context {
	return dbent.NewTxContext(context.Background(), &dbent.Tx{})
}

type oauthProxyCountryCacheStub struct {
	ProxyLatencyCache
	countries map[int64]string
}

func (c *oauthProxyCountryCacheStub) GetProxyLatencies(_ context.Context, ids []int64) (map[int64]*ProxyLatencyInfo, error) {
	result := make(map[int64]*ProxyLatencyInfo, len(ids))
	for _, id := range ids {
		if country, ok := c.countries[id]; ok {
			result[id] = &ProxyLatencyInfo{Success: true, CountryCode: country}
		}
	}
	return result, nil
}

func TestOAuthDedicatedProxyFiltersCountriesBeforeProbeLimit(t *testing.T) {
	items := make([]ProxyWithAccountCount, 0, 6)
	countries := make(map[int64]string)
	for id := int64(1); id <= 6; id++ {
		p := oauthProxyFixture(id, 0)
		p.CountryCode = ""
		items = append(items, p)
		countries[id] = "JP"
	}
	countries[6] = "US"
	repo := &oauthDedicatedProxyRepoStub{items: items}
	prober := &oauthDedicatedProberStub{results: map[string]*ProxyExitInfo{"proxy-6.test": oauthExitFixture("9.9.9.9")}}
	s := &adminServiceImpl{proxyRepo: repo, proxyProber: prober, proxyLatencyCache: &oauthProxyCountryCacheStub{countries: countries}}
	in := &CreateAccountInput{}
	require.NoError(t, s.applyOAuthDedicatedProxy(oauthProxyTestContext(), in, []string{"US"}))
	require.EqualValues(t, 6, *in.ProxyID, "five lower-ID JP entries must not consume the US candidate probe budget")
	require.Equal(t, []string{"proxy-6.test"}, prober.calls)
}

func TestOAuthDedicatedProxyRejectsSharedExitAndPreservesInputExtra(t *testing.T) {
	repo := &oauthDedicatedProxyRepoStub{items: []ProxyWithAccountCount{oauthProxyFixture(1, 1), oauthProxyFixture(2, 0), oauthProxyFixture(3, 0)}}
	prober := &oauthDedicatedProberStub{results: map[string]*ProxyExitInfo{"proxy-1.test": oauthExitFixture("8.8.8.8"), "proxy-2.test": oauthExitFixture("::ffff:8.8.8.8"), "proxy-3.test": oauthExitFixture("9.9.9.9")}}
	s := &adminServiceImpl{proxyRepo: repo, proxyProber: prober}
	requested := int64(2)
	extra := map[string]any{"preserve": "value", "openai_request_timezone": "Europe/London"}
	in := &CreateAccountInput{ProxyID: &requested, Extra: extra}
	require.NoError(t, s.applyOAuthDedicatedProxy(oauthProxyTestContext(), in, []string{"US"}))
	require.EqualValues(t, 3, *in.ProxyID)
	require.Equal(t, "America/Los_Angeles", in.Extra[openAIRequestTimezoneExtraKey])
	require.Equal(t, "value", in.Extra["preserve"])
	require.Equal(t, "Europe/London", extra[openAIRequestTimezoneExtraKey], "caller map must not be mutated")
}

func TestOAuthDedicatedProxyPrefersUnusedRequestedProxyAndPaginates(t *testing.T) {
	items := make([]ProxyWithAccountCount, 0, 102)
	for id := int64(1); id <= 101; id++ {
		p := oauthProxyFixture(id, 0)
		p.Status = "inactive"
		items = append(items, p)
	}
	items = append(items, oauthProxyFixture(102, 0))
	items[len(items)-1].CountryCode = "" // A specified unused proxy may be verified without cached geography.
	items[0].Status = StatusActive
	repo := &oauthDedicatedProxyRepoStub{items: items}
	prober := &oauthDedicatedProberStub{results: map[string]*ProxyExitInfo{"proxy-1.test": oauthExitFixture("8.8.8.8"), "proxy-102.test": oauthExitFixture("9.9.9.9")}}
	s := &adminServiceImpl{proxyRepo: repo, proxyProber: prober}
	requested := int64(102)
	in := &CreateAccountInput{ProxyID: &requested}
	require.NoError(t, s.applyOAuthDedicatedProxy(oauthProxyTestContext(), in, []string{"US"}))
	require.EqualValues(t, 102, *in.ProxyID)
	require.Equal(t, []int{1, 2}, repo.pages)
	require.Equal(t, []string{"proxy-102.test"}, prober.calls)
}

func TestOAuthDedicatedProxyFailsClosedOnUnobservableOccupiedExit(t *testing.T) {
	occupied := oauthProxyFixture(1, 1)
	occupied.Status = "inactive"
	repo := &oauthDedicatedProxyRepoStub{items: []ProxyWithAccountCount{occupied, oauthProxyFixture(2, 0)}}
	prober := &oauthDedicatedProberStub{results: map[string]*ProxyExitInfo{"proxy-2.test": oauthExitFixture("9.9.9.9")}, errors: map[string]error{"proxy-1.test": errors.New("http://secret:password@private-endpoint:407")}}
	s := &adminServiceImpl{proxyRepo: repo, proxyProber: prober}
	in := &CreateAccountInput{}
	err := s.applyOAuthDedicatedProxy(oauthProxyTestContext(), in, []string{"US"})
	require.ErrorContains(t, err, "cannot confirm occupied proxy exit: 1")
	require.NotContains(t, err.Error(), "password")
	require.Nil(t, in.ProxyID)
	require.NotContains(t, prober.calls, "proxy-2.test")
}

func TestOAuthDedicatedProxySkipsExpiredDisabledWrongCountryAndInvalidTimezone(t *testing.T) {
	expired := oauthProxyFixture(1, 0)
	past := time.Now().Add(-time.Minute)
	expired.ExpiresAt = &past
	disabled := oauthProxyFixture(2, 0)
	disabled.Status = "inactive"
	repo := &oauthDedicatedProxyRepoStub{items: []ProxyWithAccountCount{expired, disabled, oauthProxyFixture(3, 0), oauthProxyFixture(4, 0), oauthProxyFixture(5, 0)}}
	wrongCountry := oauthExitFixture("8.8.8.8")
	wrongCountry.CountryCode = "JP"
	wrongTimezone := oauthExitFixture("9.9.9.9")
	wrongTimezone.Timezone = "Untrusted/Zone"
	prober := &oauthDedicatedProberStub{results: map[string]*ProxyExitInfo{"proxy-3.test": wrongCountry, "proxy-4.test": wrongTimezone, "proxy-5.test": oauthExitFixture("1.1.1.1")}}
	s := &adminServiceImpl{proxyRepo: repo, proxyProber: prober}
	in := &CreateAccountInput{}
	require.NoError(t, s.applyOAuthDedicatedProxy(oauthProxyTestContext(), in, []string{"US"}))
	require.EqualValues(t, 5, *in.ProxyID)
	require.Equal(t, []string{"proxy-3.test", "proxy-4.test", "proxy-5.test"}, prober.calls)
}

func TestOAuthDedicatedProxyNoHealthySpareDoesNotFallBackToDirect(t *testing.T) {
	repo := &oauthDedicatedProxyRepoStub{items: []ProxyWithAccountCount{oauthProxyFixture(1, 0)}}
	prober := &oauthDedicatedProberStub{results: map[string]*ProxyExitInfo{"proxy-1.test": oauthExitFixture("127.0.0.1")}}
	s := &adminServiceImpl{proxyRepo: repo, proxyProber: prober}
	requested := int64(77)
	in := &CreateAccountInput{ProxyID: &requested}
	err := s.applyOAuthDedicatedProxy(oauthProxyTestContext(), in, []string{"US"})
	require.ErrorContains(t, err, "no healthy unused proxy")
	require.EqualValues(t, 77, *in.ProxyID)
}

func TestOAuthDedicatedProxyRequiresTransactionAndHonorsCancellation(t *testing.T) {
	repo := &oauthDedicatedProxyRepoStub{}
	prober := &oauthDedicatedProberStub{}
	s := &adminServiceImpl{proxyRepo: repo, proxyProber: prober}
	require.ErrorContains(t, s.applyOAuthDedicatedProxy(context.Background(), &CreateAccountInput{}, []string{"US"}), "requires an account transaction")
	require.False(t, repo.locked)
	ctx, cancel := context.WithCancel(oauthProxyTestContext())
	cancel()
	require.Error(t, s.applyOAuthDedicatedProxy(ctx, &CreateAccountInput{}, []string{"US"}))
	require.Empty(t, prober.calls)
}
