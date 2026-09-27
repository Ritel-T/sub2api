package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIRequiredResponseOwner_ManualPause(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		for _, bps := range []bool{false, true} {
			for _, staleSnapshot := range []bool{false, true} {
				for _, canMove := range []bool{false, true} {
					name := fmt.Sprintf("%s/bps=%t/stale=%t/movable=%t", engine, bps, staleSnapshot, canMove)
					t.Run(name, func(t *testing.T) {
						accounts := encryptedMessageCapabilityAccounts()
						if !bps {
							accounts[0].Extra = accounts[1].Extra
						}
						stale := accounts[0]
						accounts[0].Schedulable = false
						var acquired, released []int64
						svc := encryptedMessageCapabilityService(t, engine, accounts, &acquired, &released)
						svc.cfg.Gateway.OpenAIWS.LBTopK = 2
						if staleSnapshot {
							svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
								snapshotAccounts: []*Account{&stale, &accounts[1]},
								accountsByID:     map[int64]*Account{stale.ID: &stale, accounts[1].ID: &accounts[1]},
							}}
						}
						ctx := context.Background()
						store := svc.getOpenAIWSStateStore()
						const responseID = "resp_required_paused_owner"
						require.NoError(t, store.BindResponseAccount(ctx, 0, responseID, accounts[0].ID, time.Hour))
						selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, responseID, "", "gpt-6-astra", nil,
							OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, canMove, false)
						if canMove {
							require.NoError(t, err)
							require.NotNil(t, selection)
							require.Equal(t, accounts[1].ID, selection.Account.ID)
							if selection.ReleaseFunc != nil {
								selection.ReleaseFunc()
							}
						} else {
							require.ErrorIs(t, err, errOpenAIRequiredResponseOwnerUnavailable)
							require.ErrorIs(t, err, ErrNoAvailableAccounts)
							require.Nil(t, selection)
							require.Empty(t, acquired, "neither the paused owner nor the healthy backup may acquire a slot")
							owner, readErr := store.GetResponseAccount(ctx, 0, responseID)
							require.NoError(t, readErr)
							require.Equal(t, accounts[0].ID, owner)
						}
						require.ElementsMatch(t, acquired, released)
					})
				}
			}
		}
	}
}

type requiredResponseOwnerReadFailureRepo struct {
	schedulerTestOpenAIAccountRepo
	ownerID         int64
	successfulReads int
	ownerReads      int
}

type requiredResponseOwnerBindingFailureStore struct {
	OpenAIWSStateStore
	deletes int
}

func (s *requiredResponseOwnerBindingFailureStore) GetResponseAccount(context.Context, int64, string) (int64, error) {
	return 0, errors.New("synthetic response binding read failure")
}

func (s *requiredResponseOwnerBindingFailureStore) DeleteResponseAccount(ctx context.Context, groupID int64, responseID string) error {
	s.deletes++
	return s.OpenAIWSStateStore.DeleteResponseAccount(ctx, groupID, responseID)
}

func TestOpenAIRequiredResponseOwner_BindingReadFailure(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		t.Run(engine, func(t *testing.T) {
			accounts := encryptedMessageCapabilityAccounts()
			var acquired, released []int64
			svc := encryptedMessageCapabilityService(t, engine, accounts, &acquired, &released)
			ctx := context.Background()
			store := svc.getOpenAIWSStateStore()
			const responseID = "resp_required_binding_read_failure"
			require.NoError(t, store.BindResponseAccount(ctx, 0, responseID, accounts[0].ID, time.Hour))
			failing := &requiredResponseOwnerBindingFailureStore{OpenAIWSStateStore: store}
			svc.openaiWSStateStore = failing
			selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, responseID, "", "gpt-6-astra", nil,
				OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, false)
			require.ErrorIs(t, err, errOpenAIRequiredResponseOwnerUnavailable)
			require.Nil(t, selection)
			require.Empty(t, acquired)
			require.Zero(t, failing.deletes)
			owner, readErr := store.GetResponseAccount(ctx, 0, responseID)
			require.NoError(t, readErr)
			require.Equal(t, accounts[0].ID, owner)
		})
	}
}

func (r *requiredResponseOwnerReadFailureRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	if id == r.ownerID {
		r.ownerReads++
		if r.ownerReads > r.successfulReads {
			return nil, errors.New("synthetic owner read failure")
		}
	}
	return r.schedulerTestOpenAIAccountRepo.GetByID(ctx, id)
}

func TestOpenAIRequiredResponseOwner_ReadFailure(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		for _, successfulReads := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/successful_reads=%d", engine, successfulReads), func(t *testing.T) {
				accounts := encryptedMessageCapabilityAccounts()
				var acquired, released []int64
				svc := encryptedMessageCapabilityService(t, engine, accounts, &acquired, &released)
				repo := &requiredResponseOwnerReadFailureRepo{
					schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
					ownerID:                        accounts[0].ID, successfulReads: successfulReads,
				}
				svc.accountRepo = repo
				svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
					snapshotAccounts: []*Account{&accounts[0], &accounts[1]},
					accountsByID:     map[int64]*Account{accounts[0].ID: &accounts[0], accounts[1].ID: &accounts[1]},
				}}
				ctx := context.Background()
				store := svc.getOpenAIWSStateStore()
				const responseID = "resp_required_owner_read_failure"
				require.NoError(t, store.BindResponseAccount(ctx, 0, responseID, accounts[0].ID, time.Hour))
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, responseID, "", "gpt-6-astra", nil,
					OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, false)
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				require.Contains(t, err.Error(), "required response owner unavailable")
				require.Nil(t, selection)
				require.Empty(t, acquired)
				require.Greater(t, repo.ownerReads, successfulReads, "exercise the intended failing read")
				owner, readErr := store.GetResponseAccount(ctx, 0, responseID)
				require.NoError(t, readErr)
				require.Equal(t, accounts[0].ID, owner)
			})
		}
	}
}

func TestOpenAIRequiredResponseOwner_Ineligible(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		for _, reason := range []string{"excluded", "disabled", "rate_limited", "expired", "group_removed", "transport_changed", "compact_unsupported"} {
			t.Run(engine+"/"+reason, func(t *testing.T) {
				accounts := encryptedMessageCapabilityAccounts()
				var excluded map[int64]struct{}
				requireCompact := false
				switch reason {
				case "excluded":
					excluded = map[int64]struct{}{accounts[0].ID: {}}
				case "disabled":
					accounts[0].Status = "disabled"
				case "rate_limited":
					until := time.Now().Add(time.Hour)
					accounts[0].RateLimitResetAt = &until
				case "expired":
					expired := time.Now().Add(-time.Hour)
					accounts[0].ExpiresAt, accounts[0].AutoPauseOnExpired = &expired, true
				case "group_removed":
					accounts[0].GroupIDs = []int64{123}
				case "transport_changed":
					accounts[0].Extra["openai_ws_force_http"] = true
				case "compact_unsupported":
					accounts[0].Extra["openai_compact_mode"] = OpenAICompactModeForceOff
					requireCompact = true
				}
				var acquired, released []int64
				svc := encryptedMessageCapabilityService(t, engine, accounts, &acquired, &released)
				ctx := context.Background()
				store := svc.getOpenAIWSStateStore()
				const responseID = "resp_required_ineligible_owner"
				require.NoError(t, store.BindResponseAccount(ctx, 0, responseID, accounts[0].ID, time.Hour))
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, responseID, "", "gpt-6-astra", excluded,
					OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, requireCompact, false, false)
				require.ErrorIs(t, err, errOpenAIRequiredResponseOwnerUnavailable)
				require.Nil(t, selection)
				require.NotContains(t, acquired, accounts[1].ID, "a required owner must never fall through to the backup")
				require.ElementsMatch(t, acquired, released, "late compatibility rejection must release the owner slot")
				owner, readErr := store.GetResponseAccount(ctx, 0, responseID)
				require.NoError(t, readErr)
				require.Equal(t, accounts[0].ID, owner)
			})
		}
	}
}
