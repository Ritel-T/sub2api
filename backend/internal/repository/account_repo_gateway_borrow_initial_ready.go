package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"reflect"
	"time"
)

func (r *accountRepository) MarkGatewayBorrowInitialReadyIfObserved(ctx context.Context, id int64, o service.GatewayBorrowInitialReadyObservation) (service.GatewayBorrowPolicyResult, error) {
	if dbent.TxFromContext(ctx) != nil {
		return r.markGatewayBorrowInitialReadyInTx(ctx, id, o)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return r.markGatewayBorrowInitialReadyInTx(ctx, id, o)
	}
	if err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := r.markGatewayBorrowInitialReadyInTx(dbent.NewTxContext(ctx, tx), id, o)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	if result.Applied {
		r.syncSchedulerAccountSnapshotDetached(ctx, id)
	}
	return result, nil
}

func (r *accountRepository) markGatewayBorrowInitialReadyInTx(ctx context.Context, id int64, o service.GatewayBorrowInitialReadyObservation) (service.GatewayBorrowPolicyResult, error) {
	skip := func(reason string) (service.GatewayBorrowPolicyResult, error) {
		return service.GatewayBorrowPolicyResult{Skipped: true, Reason: reason}, nil
	}
	client := clientFromContext(ctx, r.client)
	var raw string
	err := scanSingleRow(ctx, client, `SELECT jsonb_build_object(
 'credentials',credentials,'extra',extra,'proxy_id',proxy_id,
 'status',status,'schedulable',schedulable,'platform',platform,'type',type,
 'parent_account_id',parent_account_id,
 'expected_config',jsonb_build_object('concurrency',concurrency,'load_factor',load_factor,
 'priority',priority,'model_mapping',credentials->'model_mapping',
 'group_ids',(SELECT COALESCE(jsonb_agg(group_id ORDER BY group_id),'[]'::jsonb) FROM account_groups WHERE account_id=$1)))::text
 FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, []any{id}, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return skip("account_unavailable")
	}
	if err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	var row struct {
		Credentials map[string]any `json:"credentials"`
		Extra       map[string]any `json:"extra"`
		ProxyID     *int64         `json:"proxy_id"`
		Status      string         `json:"status"`
		Schedulable bool           `json:"schedulable"`
		Platform    string         `json:"platform"`
		Type        string         `json:"type"`
		ParentID    *int64         `json:"parent_account_id"`
		Config      map[string]any `json:"expected_config"`
	}
	if err = json.Unmarshal([]byte(raw), &row); err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	a := &service.Account{Platform: row.Platform, Type: row.Type, ParentAccountID: row.ParentID, Credentials: row.Credentials, Extra: row.Extra, ProxyID: row.ProxyID}
	// Configuration has already been loaded under the account row lock.
	configRaw, _ := json.Marshal(row.Config)
	var defaults struct {
		Concurrency int     `json:"concurrency"`
		Priority    int     `json:"priority"`
		LoadFactor  *int    `json:"load_factor"`
		GroupIDs    []int64 `json:"group_ids"`
	}
	if err = json.Unmarshal(configRaw, &defaults); err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	a.Concurrency, a.Priority, a.LoadFactor, a.GroupIDs = defaults.Concurrency, defaults.Priority, defaults.LoadFactor, defaults.GroupIDs
	if !a.IsOpenAIOAuth() || a.IsShadow() || a.IsOpenAIAgentIdentity() || a.IsOpenAIPersonalAccessToken() {
		return skip("account_unavailable")
	}
	if row.Status != service.StatusActive || !row.Schedulable {
		return skip("paused_or_inactive")
	}
	if time.Since(o.ObservedAt) > 10*time.Minute {
		return skip("stale_observation")
	}
	if !reflect.DeepEqual(row.ProxyID, o.ExpectedProxyID) || service.GatewayBorrowCredentialSHA256(row.Credentials) != o.CredentialSHA256 {
		return skip("identity_changed")
	}
	b, err := json.Marshal(o.ExpectedConfig)
	if err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	var expectedConfig map[string]any
	if err = json.Unmarshal(b, &expectedConfig); err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	if !reflect.DeepEqual(row.Config, expectedConfig) {
		return skip("initial_configuration_changed")
	}
	if row.Extra[service.GatewayBorrowAccountQualityModeKey] != service.GatewayBorrowAccountQualityMode {
		return skip("policy_mode_changed")
	}
	if row.Extra[service.GatewayBorrowAccountQualityPendingKey] != true {
		return skip("already_classified")
	}
	expected := service.GatewayBorrowPolicyProjection(o.ExpectedPolicy)
	// A retried successful mark may observe only this false->true difference.
	current := service.GatewayBorrowPolicyProjection(row.Extra)
	expected[service.GatewayBorrowInitialReadyKey] = current[service.GatewayBorrowInitialReadyKey]
	expected[service.GatewayBorrowInitialReadyFingerprintKey] = current[service.GatewayBorrowInitialReadyFingerprintKey]
	b, err = json.Marshal(expected)
	if err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	var normalized map[string]any
	if err = json.Unmarshal(b, &normalized); err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	if !reflect.DeepEqual(current, normalized) {
		return skip("policy_changed")
	}
	if service.GatewayBorrowInitialReadyMatchesCurrent(a) {
		return skip("unchanged")
	}
	if token, ok := row.Credentials["access_token"].(string); !ok || token == "" {
		return skip("account_unavailable")
	}
	if !a.IsOpenAIGatewayAccountQualityPendingForUpstreamModel("gpt-6-astra") {
		return skip("astra_not_configured")
	}
	var changed int64
	updateRaw, err := json.Marshal(map[string]any{service.GatewayBorrowInitialReadyKey: true, service.GatewayBorrowInitialReadyFingerprintKey: service.GatewayBorrowInitialReadyFingerprint(a)})
	if err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	err = scanSingleRow(ctx, client, `UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb)||$2::jsonb,updated_at=NOW() WHERE id=$1 RETURNING id`, []any{id, string(updateRaw)}, &changed)
	if err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	if err = enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	return service.GatewayBorrowPolicyResult{Applied: true, Reason: "initial_defaults_ready"}, nil
}
