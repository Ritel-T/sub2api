//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const adminWaitYieldAccountID int64 = 71

// The mutex protects the compare-and-increment as one operation, matching the
// account-wide admission contract. This stub does not claim to test Redis Lua.
type adminWaitYieldCache struct {
	helperConcurrencyCacheStub
	waiting              map[int64]int
	limits               []int
	increments           int
	admissions           int
	decrements           int
	highWater            int
	decrementContextErrs []error
	incrementErr         error
	incrementErrorResult bool
	incremented          chan struct{}
}

type adminWaitYieldSnapshot struct {
	waiting, increments, admissions, decrements, highWater int
	acquires, releases                                     int
	limits                                                 []int
	decrementContextErrs                                   []error
}

func newAdminWaitYieldCache(waiting int, slotSequence ...bool) *adminWaitYieldCache {
	return &adminWaitYieldCache{
		helperConcurrencyCacheStub: helperConcurrencyCacheStub{accountSeq: slotSequence},
		waiting:                    map[int64]int{adminWaitYieldAccountID: waiting},
		highWater:                  waiting,
	}
}

func (s *adminWaitYieldCache) IncrementAccountWaitCount(_ context.Context, accountID int64, maxWait int) (bool, error) {
	s.mu.Lock()
	s.increments++
	s.limits = append(s.limits, maxWait)
	allowed, err := false, s.incrementErr
	if err != nil {
		allowed = s.incrementErrorResult
	} else if s.waiting[accountID] < maxWait {
		s.waiting[accountID]++
		s.admissions++
		allowed = true
		if s.waiting[accountID] > s.highWater {
			s.highWater = s.waiting[accountID]
		}
	}
	s.mu.Unlock()
	if s.incremented != nil {
		s.incremented <- struct{}{}
	}
	return allowed, err
}

func (s *adminWaitYieldCache) DecrementAccountWaitCount(ctx context.Context, accountID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decrements++
	s.decrementContextErrs = append(s.decrementContextErrs, ctx.Err())
	// Deliberately do not clamp: an unowned or duplicate decrement must fail
	// these tests instead of being hidden by a floor at zero.
	s.waiting[accountID]--
	return nil
}

func (s *adminWaitYieldCache) GetAccountWaitingCount(_ context.Context, accountID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiting[accountID], nil
}

func (s *adminWaitYieldCache) snapshot() adminWaitYieldSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return adminWaitYieldSnapshot{
		waiting: s.waiting[adminWaitYieldAccountID], increments: s.increments,
		admissions: s.admissions, decrements: s.decrements, highWater: s.highWater,
		acquires: s.accountAcquireCalls, releases: s.accountReleaseCalls,
		limits:               append([]int(nil), s.limits...),
		decrementContextErrs: append([]error(nil), s.decrementContextErrs...),
	}
}

func newAdminWaitYieldContext(ctx context.Context, role string, keyID int64) (*gin.Context, *httptest.ResponseRecorder) {
	c, rec := newHelperTestContext(http.MethodPost, "/v1/responses")
	c.Request = c.Request.WithContext(ctx)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42, Concurrency: 50})
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID: keyID, UserID: 42, User: &service.User{ID: 42, Role: role},
	})
	if role != "" {
		c.Set(string(middleware.ContextKeyUserRole), role)
	}
	return c, rec
}

func newAdminWaitYieldHandler(cache *adminWaitYieldCache) *OpenAIGatewayHandler {
	return &OpenAIGatewayHandler{
		gatewayService: &service.OpenAIGatewayService{},
		concurrencyHelper: NewConcurrencyHelper(
			service.NewConcurrencyService(cache), SSEPingFormatNone, 0,
		),
	}
}

func newAdminWaitYieldSelection(maxWaiting int) *service.AccountSelectionResult {
	return &service.AccountSelectionResult{
		Account: &service.Account{
			ID: adminWaitYieldAccountID, Platform: service.PlatformOpenAI,
			Type: service.AccountTypeOAuth, Status: service.StatusActive,
			Schedulable: true, Concurrency: 2,
		},
		WaitPlan: &service.AccountWaitPlan{
			AccountID: adminWaitYieldAccountID, MaxConcurrency: 2,
			Timeout: 5 * time.Second, MaxWaiting: maxWaiting,
		},
	}
}

type adminWaitYieldOutcome struct {
	release func()
	result  openAISlotAcquireResult
}

func runAdminWaitYieldHTTP(h *OpenAIGatewayHandler, c *gin.Context, selection *service.AccountSelectionResult) adminWaitYieldOutcome {
	started := false
	release, result := h.acquireResponsesAccountSlot(c, nil, "", selection, false, &started, zap.NewNop())
	return adminWaitYieldOutcome{release: release, result: result}
}

func startAdminWaitYieldHTTP(h *OpenAIGatewayHandler, c *gin.Context, selection *service.AccountSelectionResult) <-chan adminWaitYieldOutcome {
	done := make(chan adminWaitYieldOutcome, 1)
	go func() { done <- runAdminWaitYieldHTTP(h, c, selection) }()
	return done
}

func awaitAdminWaitYieldOutcome(t *testing.T, done <-chan adminWaitYieldOutcome) adminWaitYieldOutcome {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("account acquisition did not finish after release/cancellation")
		return adminWaitYieldOutcome{}
	}
}

func awaitAdminWaitYieldIncrement(t *testing.T, incremented <-chan struct{}) {
	t.Helper()
	select {
	case <-incremented:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach account wait admission")
	}
}

func assertAdminWaitYieldHTTPError(t *testing.T, rec *httptest.ResponseRecorder, status int, errType, code string) {
	t.Helper()
	require.Equal(t, status, rec.Code, rec.Body.String())
	var envelope struct {
		Error struct{ Type, Code, Message string }
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, errType, envelope.Error.Type)
	require.Equal(t, code, envelope.Error.Code)
	require.NotEmpty(t, envelope.Error.Message)
}

func TestAdminWaitYieldCallerIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		role   any
		nilGin bool
		admin  bool
	}{
		{name: "trusted_admin", role: service.RoleAdmin, admin: true},
		{name: "trusted_user", role: service.RoleUser},
		{name: "observer", role: service.RoleObserver},
		{name: "missing_role"},
		{name: "empty_role", role: ""},
		{name: "wrong_type", role: 123},
		{name: "wrong_case", role: "ADMIN"},
		{name: "nil_context", nilGin: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c *gin.Context
			if !tc.nilGin {
				c, _ = newAdminWaitYieldContext(context.Background(), "", 1)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses?role=admin&user_role=admin",
					strings.NewReader("{\"role\":\"admin\",\"user_role\":\"admin\"}"))
				c.Request.Header.Set("X-User-Role", service.RoleAdmin)
				c.Set("role", service.RoleAdmin)
				// Even another object claiming admin must not replace the
				// authenticated role installed by middleware.
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
					ID: 1, UserID: 42, User: &service.User{ID: 42, Role: service.RoleAdmin},
				})
				if tc.role != nil {
					c.Set(string(middleware.ContextKeyUserRole), tc.role)
				}
			}
			require.Equal(t, tc.admin, isAdminAccountWaitCaller(c))
			for _, configured := range []int{-1, 0, 1, 3, 100} {
				want := configured
				if tc.admin && configured > 1 {
					want = 1
				}
				require.Equal(t, want, accountWaitLimitForCaller(c, configured), "configured=%d", configured)
			}
		})
	}
}

func TestAdminWaitYieldHTTPAdmissionThresholds(t *testing.T) {
	for _, tc := range []struct {
		name       string
		role       string
		configured int
		limit      int
		waiting    []int
	}{
		{"sticky_admin", service.RoleAdmin, 3, 1, []int{0, 1, 2, 3}},
		{"sticky_user", service.RoleUser, 3, 3, []int{0, 1, 2, 3}},
		{"fallback_admin", service.RoleAdmin, 100, 1, []int{0, 1, 2, 3, 99, 100}},
		{"fallback_user", service.RoleUser, 100, 100, []int{0, 1, 2, 3, 99, 100}},
	} {
		for _, waiting := range tc.waiting {
			t.Run(fmt.Sprintf("%s/W%d", tc.name, waiting), func(t *testing.T) {
				cache := newAdminWaitYieldCache(waiting, false, true)
				c, rec := newAdminWaitYieldContext(context.Background(), tc.role, 1)
				out := runAdminWaitYieldHTTP(newAdminWaitYieldHandler(cache), c, newAdminWaitYieldSelection(tc.configured))
				state := cache.snapshot()
				require.Equal(t, []int{tc.limit}, state.limits)
				require.Equal(t, waiting, state.waiting, "only this request's wait ownership may be released")
				if waiting < tc.limit {
					require.Equal(t, openAISlotAcquireOK, out.result)
					require.NotNil(t, out.release)
					require.Zero(t, rec.Body.Len())
					require.Equal(t, 2, state.acquires)
					require.Equal(t, 1, state.admissions)
					require.Equal(t, 1, state.decrements)
					require.Zero(t, state.releases, "acquired slot must stay held until forwarding finishes")
					out.release()
					out.release()
					require.Equal(t, 1, cache.snapshot().releases)
				} else {
					require.Equal(t, openAISlotAcquireFailed, out.result)
					require.Nil(t, out.release)
					assertAdminWaitYieldHTTPError(t, rec, http.StatusTooManyRequests, "rate_limit_error", gatewayQueueFullCode)
					require.Equal(t, 1, state.acquires, "a rejected waiter must not continue polling")
					require.Zero(t, state.admissions)
					require.Zero(t, state.decrements)
					require.Zero(t, state.releases)
				}
			})
		}
	}
}

func TestAdminWaitYieldHTTPImmediateSlotSkipsWaiting(t *testing.T) {
	for _, role := range []string{service.RoleAdmin, service.RoleUser} {
		for _, acquired := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/selection_acquired_%t", role, acquired), func(t *testing.T) {
				cache := newAdminWaitYieldCache(3, true)
				c, rec := newAdminWaitYieldContext(context.Background(), role, 1)
				selection := newAdminWaitYieldSelection(3)
				selection.Acquired = acquired
				if acquired {
					selection.WaitPlan = nil
					selection.ReleaseFunc = func() {
						_ = cache.ReleaseAccountSlot(context.Background(), adminWaitYieldAccountID, "scheduler-owned-slot")
					}
				}
				out := runAdminWaitYieldHTTP(newAdminWaitYieldHandler(cache), c, selection)
				require.Equal(t, openAISlotAcquireOK, out.result)
				require.NotNil(t, out.release)
				require.Zero(t, rec.Body.Len())
				state := cache.snapshot()
				wantAcquires := 1
				if acquired {
					wantAcquires = 0
				}
				require.Equal(t, wantAcquires, state.acquires)
				require.Zero(t, state.increments)
				require.Zero(t, state.decrements)
				require.Equal(t, 3, state.waiting)
				out.release()
				out.release()
				require.Equal(t, 1, cache.snapshot().releases)
			})
		}
	}
}

func TestAdminWaitYieldHTTPConcurrentKeysShareAccountCount(t *testing.T) {
	for _, tc := range []struct {
		name       string
		role       string
		configured int
		limit      int
	}{
		{"admin_sticky", service.RoleAdmin, 3, 1},
		{"admin_fallback", service.RoleAdmin, 100, 1},
		{"customer_sticky", service.RoleUser, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const requests = 8
			cache := newAdminWaitYieldCache(0)
			cache.incremented = make(chan struct{}, requests)
			h := newAdminWaitYieldHandler(cache)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			start := make(chan struct{})
			done := make([]chan adminWaitYieldOutcome, requests)
			recorders := make([]*httptest.ResponseRecorder, requests)
			for i := 0; i < requests; i++ {
				// Same authenticated user, distinct API keys, one upstream account.
				c, rec := newAdminWaitYieldContext(ctx, tc.role, int64(100+i))
				recorders[i] = rec
				done[i] = make(chan adminWaitYieldOutcome, 1)
				go func(c *gin.Context, result chan<- adminWaitYieldOutcome) {
					<-start
					result <- runAdminWaitYieldHTTP(h, c, newAdminWaitYieldSelection(tc.configured))
				}(c, done[i])
			}
			close(start)
			for i := 0; i < requests; i++ {
				awaitAdminWaitYieldIncrement(t, cache.incremented)
			}
			state := cache.snapshot()
			require.Equal(t, requests, state.increments)
			require.Equal(t, tc.limit, state.waiting)
			require.Equal(t, tc.limit, state.highWater, "simultaneous keys must not over-admit")
			require.Equal(t, tc.limit, state.admissions)
			require.Zero(t, state.decrements, "accepted requests must remain registered while blocked")
			for _, limit := range state.limits {
				require.Equal(t, tc.limit, limit)
			}

			cancel()
			rejected, cancelled := 0, 0
			for i, result := range done {
				out := awaitAdminWaitYieldOutcome(t, result)
				require.Equal(t, openAISlotAcquireFailed, out.result)
				require.Nil(t, out.release)
				switch recorders[i].Code {
				case http.StatusTooManyRequests:
					rejected++
					assertAdminWaitYieldHTTPError(t, recorders[i], http.StatusTooManyRequests, "rate_limit_error", gatewayQueueFullCode)
				case statusClientClosedRequest:
					cancelled++
				default:
					t.Fatalf("unexpected status %d: %s", recorders[i].Code, recorders[i].Body.String())
				}
			}
			require.Equal(t, requests-tc.limit, rejected)
			require.Equal(t, tc.limit, cancelled)
			state = cache.snapshot()
			require.Zero(t, state.waiting)
			require.Equal(t, tc.limit, state.decrements)
			require.Zero(t, state.releases)
			for _, err := range state.decrementContextErrs {
				require.NoError(t, err, "cleanup must use an uncancelled context")
			}
		})
	}
}

func TestAdminWaitYieldHTTPWaitingLifecycle(t *testing.T) {
	for _, role := range []string{service.RoleAdmin, service.RoleUser} {
		for _, exit := range []string{"success_after_poll", "cancel", "timeout"} {
			t.Run(role+"/"+exit, func(t *testing.T) {
				waiting := 0
				if role == service.RoleUser {
					waiting = 2 // Other requests' counts must survive our cleanup.
				}
				cache := newAdminWaitYieldCache(waiting)
				if exit == "success_after_poll" {
					cache.accountSeq = []bool{false, false, true}
				}
				cache.incremented = make(chan struct{}, 1)
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				c, rec := newAdminWaitYieldContext(ctx, role, 1)
				selection := newAdminWaitYieldSelection(3)
				if exit == "timeout" {
					selection.WaitPlan.Timeout = 10 * time.Millisecond
				}
				done := startAdminWaitYieldHTTP(newAdminWaitYieldHandler(cache), c, selection)
				awaitAdminWaitYieldIncrement(t, cache.incremented)
				if exit == "cancel" {
					require.Equal(t, waiting+1, cache.snapshot().waiting)
					cancel()
				}
				out := awaitAdminWaitYieldOutcome(t, done)
				state := cache.snapshot()
				require.Equal(t, waiting, state.waiting)
				require.Equal(t, 1, state.decrements)
				require.Equal(t, []error{nil}, state.decrementContextErrs)
				require.Zero(t, state.releases)
				if exit == "success_after_poll" {
					require.Equal(t, openAISlotAcquireOK, out.result)
					require.NotNil(t, out.release)
					require.Equal(t, 3, state.acquires, "exercise the polling loop, not just its immediate retry")
					require.Zero(t, rec.Body.Len())
					cancel()
					require.Eventually(t, func() bool { return cache.snapshot().releases == 1 }, time.Second, time.Millisecond)
					out.release()
					out.release()
					require.Equal(t, 1, cache.snapshot().releases)
					require.Equal(t, 1, cache.snapshot().decrements)
				} else {
					require.Equal(t, openAISlotAcquireFailed, out.result)
					require.Nil(t, out.release)
					if exit == "cancel" {
						assertAdminWaitYieldHTTPError(t, rec, statusClientClosedRequest, "api_error", "")
					} else {
						assertAdminWaitYieldHTTPError(t, rec, http.StatusTooManyRequests, "rate_limit_error", gatewayConcurrencyLimitCode)
					}
				}
			})
		}
	}
}

func TestAdminWaitYieldHTTPCounterError(t *testing.T) {
	for _, rawResult := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			role string
			exit string
		}{
			{"admin_stops", service.RoleAdmin, "unavailable"},
			{"customer_continues", service.RoleUser, "success"},
			{"customer_cancel", service.RoleUser, "cancel"},
			{"customer_timeout", service.RoleUser, "timeout"},
		} {
			t.Run(fmt.Sprintf("%s/cache_result_%t", tc.name, rawResult), func(t *testing.T) {
				cache := newAdminWaitYieldCache(2)
				cache.incrementErr = errors.New("wait counter connection lost")
				cache.incrementErrorResult = rawResult
				cache.incremented = make(chan struct{}, 1)
				if tc.exit == "success" || tc.exit == "unavailable" {
					cache.accountSeq = []bool{false, true}
				}
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				c, rec := newAdminWaitYieldContext(ctx, tc.role, 1)
				selection := newAdminWaitYieldSelection(3)
				if tc.exit == "timeout" {
					selection.WaitPlan.Timeout = 10 * time.Millisecond
				}
				done := startAdminWaitYieldHTTP(newAdminWaitYieldHandler(cache), c, selection)
				awaitAdminWaitYieldIncrement(t, cache.incremented)
				if tc.exit == "cancel" {
					cancel()
				}
				out := awaitAdminWaitYieldOutcome(t, done)
				state := cache.snapshot()
				require.Equal(t, 2, state.waiting, "unconfirmed admission must not decrement another request")
				require.Equal(t, 1, state.increments)
				require.Zero(t, state.admissions)
				require.Zero(t, state.decrements)
				require.Zero(t, state.releases)
				switch tc.exit {
				case "unavailable":
					require.Equal(t, openAISlotAcquireFailed, out.result)
					require.Nil(t, out.release)
					assertAdminWaitYieldHTTPError(t, rec, http.StatusServiceUnavailable, "api_error", "")
					require.Equal(t, 1, state.acquires, "admin must stop before a second slot attempt")
				case "success":
					require.Equal(t, openAISlotAcquireOK, out.result)
					require.NotNil(t, out.release)
					require.Zero(t, rec.Body.Len())
					require.Equal(t, 2, state.acquires)
					out.release()
					require.Equal(t, 1, cache.snapshot().releases)
				case "cancel":
					require.Equal(t, openAISlotAcquireFailed, out.result)
					require.Nil(t, out.release)
					assertAdminWaitYieldHTTPError(t, rec, statusClientClosedRequest, "api_error", "")
				case "timeout":
					require.Equal(t, openAISlotAcquireFailed, out.result)
					require.Nil(t, out.release)
					assertAdminWaitYieldHTTPError(t, rec, http.StatusTooManyRequests, "rate_limit_error", gatewayConcurrencyLimitCode)
				}
			})
		}
	}
}

func TestAdminWaitYieldMissingCounterPreservesError(t *testing.T) {
	helper := NewConcurrencyHelper(service.NewConcurrencyService(nil), SSEPingFormatNone, 0)
	allowed, err := helper.IncrementAccountWaitCount(context.Background(), adminWaitYieldAccountID, 1)
	require.True(t, allowed, "customer callers retain best-effort admission")
	require.Error(t, err, "admin callers need an observable error to stop")
	status, _, _, _ := concurrencyErrorResponse(err, "account")
	require.Equal(t, http.StatusServiceUnavailable, status)
}

func TestAdminWaitYieldLegacyWrapper(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		role                       string
		waiting                    int
		slots                      []bool
		counterErr                 bool
		wantIncrements, wantGrants int
		wantAcquires               int
		wantStatus                 int
	}{
		{"admin_empty_slot", service.RoleAdmin, 3, []bool{true}, false, 0, 0, 1, 0},
		{"customer_empty_slot", service.RoleUser, 3, []bool{true}, false, 0, 0, 1, 0},
		{"admin_waits", service.RoleAdmin, 0, []bool{false, true}, false, 1, 1, 2, 0},
		{"admin_existing_waiter_rejected", service.RoleAdmin, 1, []bool{false, true}, false, 1, 0, 1, http.StatusTooManyRequests},
		{"customer_keeps_uncounted_wait", service.RoleUser, 3, []bool{false, true}, false, 0, 0, 2, 0},
		{"admin_counter_error", service.RoleAdmin, 0, []bool{false, true}, true, 1, 0, 1, http.StatusServiceUnavailable},
		{"customer_does_not_consult_counter", service.RoleUser, 3, []bool{false, true}, true, 0, 0, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := newAdminWaitYieldCache(tc.waiting, tc.slots...)
			if tc.counterErr {
				cache.incrementErr = errors.New("counter unavailable")
			}
			c, rec := newAdminWaitYieldContext(context.Background(), tc.role, 1)
			started := false
			release, err := newAdminWaitYieldHandler(cache).concurrencyHelper.AcquireAccountSlotWithAdminWaitTimeout(
				c, adminWaitYieldAccountID, 2, time.Second, false, &started,
			)
			state := cache.snapshot()
			require.Equal(t, tc.waiting, state.waiting)
			require.Equal(t, tc.wantIncrements, state.increments)
			require.Equal(t, tc.wantGrants, state.admissions)
			require.Equal(t, tc.wantGrants, state.decrements)
			require.Equal(t, tc.wantAcquires, state.acquires)
			require.Zero(t, state.releases)
			require.Zero(t, rec.Body.Len(), "legacy helper leaves response writing to its caller")
			require.False(t, started)
			if tc.wantIncrements > 0 {
				require.Equal(t, []int{1}, state.limits)
			}
			if tc.wantStatus == 0 {
				require.NoError(t, err)
				require.NotNil(t, release)
				release()
				require.Equal(t, 1, cache.snapshot().releases)
			} else {
				require.Error(t, err)
				require.Nil(t, release)
				status, _, code, _ := concurrencyErrorResponse(err, "account")
				require.Equal(t, tc.wantStatus, status)
				if tc.wantStatus == http.StatusTooManyRequests {
					var queueErr *WaitQueueFullError
					require.ErrorAs(t, err, &queueErr)
					require.Equal(t, gatewayQueueFullCode, code)
				} else {
					require.ErrorIs(t, err, cache.incrementErr)
				}
			}
		})
	}
}

func TestAdminWaitYieldLegacyCancellation(t *testing.T) {
	for _, role := range []string{service.RoleAdmin, service.RoleUser} {
		t.Run(role, func(t *testing.T) {
			cache := newAdminWaitYieldCache(0)
			cache.incremented = make(chan struct{}, 1)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			c, _ := newAdminWaitYieldContext(ctx, role, 1)
			helper := newAdminWaitYieldHandler(cache).concurrencyHelper
			type result struct {
				release func()
				err     error
			}
			done := make(chan result, 1)
			go func() {
				started := false
				release, err := helper.AcquireAccountSlotWithAdminWaitTimeout(
					c, adminWaitYieldAccountID, 2, time.Second, false, &started,
				)
				done <- result{release, err}
			}()
			if role == service.RoleAdmin {
				awaitAdminWaitYieldIncrement(t, cache.incremented)
				require.Equal(t, 1, cache.snapshot().waiting)
			} else {
				require.Eventually(t, func() bool { return cache.snapshot().acquires >= 1 }, time.Second, time.Millisecond)
				require.Zero(t, cache.snapshot().increments)
			}
			cancel()
			select {
			case out := <-done:
				require.Nil(t, out.release)
				require.ErrorIs(t, out.err, context.Canceled)
			case <-time.After(3 * time.Second):
				t.Fatal("legacy wait did not exit after cancellation")
			}
			state := cache.snapshot()
			require.Zero(t, state.waiting)
			require.Zero(t, state.releases)
			if role == service.RoleAdmin {
				require.Equal(t, 1, state.decrements)
				require.Equal(t, []error{nil}, state.decrementContextErrs)
			} else {
				require.Zero(t, state.decrements)
			}
		})
	}
}
