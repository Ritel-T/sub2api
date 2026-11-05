package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const nativeOAuthAccountDefaultsIdentityKey = "openai_account_defaults_identity_fingerprint"

// Token refresh may advance initial readiness only for the same configured
// workspace and user. This creation proof excludes rotating access tokens.
func nativeOAuthAccountDefaultsIdentityFingerprint(a *Account) string {
	if a == nil || a.GetCredential("chatgpt_account_id") == "" {
		return ""
	}
	raw, err := json.Marshal(map[string]any{
		"provider_owner": a.GetCredential("chatgpt_account_id"),
		"provider_user":  a.GetCredential("chatgpt_user_id"),
		"proxy_id":       a.ProxyID, "config": GatewayBorrowInitialConfiguration(a),
	})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func nativeOAuthAccountDefaultsIdentityMatches(a *Account) bool {
	if a == nil {
		return false
	}
	proof, _ := a.Extra[nativeOAuthAccountDefaultsIdentityKey].(string)
	return proof != "" && proof == nativeOAuthAccountDefaultsIdentityFingerprint(a)
}

type oauthCreationConfigKey struct{}
type nativeOAuthCreationKey struct{}

// One creation uses one policy snapshot, including the transaction decision.
func getOAuthAutoConfigForCreation(ctx context.Context, settings *SettingService) (OAuthAutoConfig, error) {
	if c, ok := ctx.Value(oauthCreationConfigKey{}).(OAuthAutoConfig); ok {
		return c, nil
	}
	return settings.GetOAuthAutoConfig(ctx)
}

func nativeOAuthAccountCreation(ctx context.Context) bool {
	native, _ := ctx.Value(nativeOAuthCreationKey{}).(bool)
	return native
}

func (s *adminServiceImpl) oauthAccountCreationContext(ctx context.Context, input *CreateAccountInput) (context.Context, bool, error) {
	if input == nil {
		return ctx, false, ErrAccountNilInput
	}
	if !openAIAccountDefaultsInputEligible(input) || s.settingService == nil {
		return ctx, false, nil
	}
	if _, observer := ObserverGroupIDs(ctx); observer {
		return ctx, false, nil
	}
	if adminaccess.FromContext(ctx) != nil {
		return ctx, false, nil
	}
	c, err := getOAuthAutoConfigForCreation(ctx, s.settingService)
	if err != nil {
		return ctx, false, fmt.Errorf("read automatic configuration: %w", err)
	}
	ctx = context.WithValue(ctx, oauthCreationConfigKey{}, c)
	native := c.Enabled && c.Platform == PlatformOpenAI && c.OpenAIAccountDefaults != nil && c.OpenAIAccountDefaults.Enabled
	if native {
		ctx = context.WithValue(ctx, nativeOAuthCreationKey{}, true)
	}
	return ctx, native, nil
}

func (s *adminServiceImpl) withNativeOAuthCreationTransaction(ctx context.Context, create func(context.Context) (*Account, error)) (*Account, error) {
	if dbent.TxFromContext(ctx) != nil {
		return create(ctx)
	}
	if s.entClient == nil {
		return nil, infraerrors.ServiceUnavailable("AUTO_CONFIG_TRANSACTION_UNAVAILABLE", "automatic account configuration transaction is unavailable")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	account, err := create(dbent.NewTxContext(ctx, tx))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return account, nil
}

func (s *adminServiceImpl) withOAuthAccountCreation(ctx context.Context, input *CreateAccountInput, create func(context.Context) (*Account, error)) (*Account, error) {
	creationCtx, native, err := s.oauthAccountCreationContext(ctx, input)
	if err != nil {
		return nil, err
	}
	if native {
		return s.withNativeOAuthCreationTransaction(creationCtx, create)
	}
	return create(creationCtx)
}

// The row and group bindings have already been written in this transaction.
// Reuse the existing identity/configuration CAS and its scheduler outbox.
func markNativeOAuthInitialReady(ctx context.Context, repo AccountRepository, account *Account) (*Account, error) {
	current, err := repo.GetByID(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if nativeOAuthAccountCreation(ctx) {
		if fingerprint := nativeOAuthAccountDefaultsIdentityFingerprint(current); fingerprint != "" {
			if err := repo.UpdateExtra(ctx, current.ID, map[string]any{nativeOAuthAccountDefaultsIdentityKey: fingerprint}); err != nil {
				return nil, err
			}
			current, err = repo.GetByID(ctx, current.ID)
			if err != nil {
				return nil, err
			}
		}
	}
	if !current.gatewayBorrowAccountQualityPending() || !current.gatewayBorrowAccountHasAstraPermission() {
		return current, nil
	}
	if !nativeOAuthAccountCreation(ctx) && !nativeOAuthAccountDefaultsIdentityMatches(current) {
		return nil, infraerrors.Conflict("AUTO_CONFIG_INITIAL_READY_CHANGED", "account identity or initial configuration changed")
	}
	allowPaused := nativeOAuthAccountCreation(ctx)
	if (current.Status != StatusActive || !current.Schedulable) && !allowPaused {
		return current, nil
	}
	ready, ok := repo.(GatewayBorrowInitialReadyRepository)
	if !ok {
		return nil, infraerrors.ServiceUnavailable("AUTO_CONFIG_INITIAL_READY_UNAVAILABLE", "automatic account readiness persistence is unavailable")
	}
	result, err := ready.MarkGatewayBorrowInitialReadyIfObserved(ctx, current.ID, GatewayBorrowInitialReadyObservation{
		ObservedAt: time.Now().UTC(), ExpectedProxyID: current.ProxyID, AllowPaused: allowPaused,
		CredentialSHA256: GatewayBorrowCredentialSHA256(current.Credentials),
		ExpectedConfig:   GatewayBorrowInitialConfiguration(current),
		ExpectedPolicy:   GatewayBorrowPolicyProjection(current.Extra),
	})
	if err != nil {
		return nil, err
	}
	if !result.Applied && result.Reason != "unchanged" {
		return nil, infraerrors.Conflict("AUTO_CONFIG_INITIAL_READY_CHANGED", "account configuration changed before initial readiness was confirmed")
	}
	after, err := repo.GetByID(ctx, current.ID)
	if err != nil {
		return nil, err
	}
	if !GatewayBorrowInitialReadyMatchesCurrent(after) {
		return nil, infraerrors.Conflict("AUTO_CONFIG_INITIAL_READY_CHANGED", "account initial readiness readback did not match")
	}
	return after, nil
}

// CRS keeps its imported status and scheduling intent while sharing the native
// policy, proxy lock and atomic creation path used by ordinary admin imports.
func (s *adminServiceImpl) createNativeSyncedAccount(ctx context.Context, account *Account) (bool, error) {
	if account == nil {
		return false, ErrAccountNilInput
	}
	if account.IsShadow() {
		return false, nil
	}
	input := &CreateAccountInput{Platform: account.Platform, Type: account.Type,
		Priority: account.Priority, Concurrency: account.Concurrency, LoadFactor: account.LoadFactor,
		Extra: account.Extra, Credentials: account.Credentials, ProxyID: account.ProxyID}
	creationCtx, native, err := s.oauthAccountCreationContext(ctx, input)
	if err != nil || !native {
		return native, err
	}
	created, err := s.withNativeOAuthCreationTransaction(creationCtx, func(txctx context.Context) (*Account, error) {
		if err := s.ApplyOAuthAutoConfig(txctx, input); err != nil {
			return nil, err
		}
		next := *account
		next.InitialQualityPlan = input.InitialQualityPlan
		next.Priority, next.Concurrency, next.LoadFactor = input.Priority, input.Concurrency, input.LoadFactor
		next.Extra, next.Credentials, next.ProxyID = input.Extra, input.Credentials, input.ProxyID
		next.GroupIDs = append([]int64(nil), input.GroupIDs...)
		prepareGatewayBorrowAccountQualityForCreate(&next)
		if err := s.accountRepo.Create(txctx, &next); err != nil {
			return nil, err
		}
		if len(input.GroupIDs) > 0 {
			if err := s.accountRepo.BindGroups(txctx, next.ID, input.GroupIDs); err != nil {
				return nil, err
			}
		}
		ready, err := markNativeOAuthInitialReady(txctx, s.accountRepo, &next)
		if err != nil {
			return nil, err
		}
		AfterAccountCommit(txctx, func(committed context.Context) {
			recordAutoConfigInitial(committed, s.accountRepo, ready, input.GroupIDs)
		})
		return ready, nil
	})
	if err != nil {
		return true, err
	}
	*account = *created
	return true, nil
}
