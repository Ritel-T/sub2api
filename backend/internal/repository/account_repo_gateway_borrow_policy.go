package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *accountRepository) UpdateGatewayBorrowPolicyIfObserved(ctx context.Context, id int64, o service.GatewayBorrowPolicyObservation) (service.GatewayBorrowPolicyResult, error) {
	if dbent.TxFromContext(ctx) != nil {
		return r.updateGatewayBorrowPolicyInTx(ctx, id, o)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return r.updateGatewayBorrowPolicyInTx(ctx, id, o)
	}
	if err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := r.updateGatewayBorrowPolicyInTx(dbent.NewTxContext(ctx, tx), id, o)
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

// Identity and owned policy are checked under one row lock. Merge and outbox
// are in that same transaction; a concurrent quota/cooldown write is preserved.
func (r *accountRepository) updateGatewayBorrowPolicyInTx(ctx context.Context, id int64, o service.GatewayBorrowPolicyObservation) (service.GatewayBorrowPolicyResult, error) {
	skipped := func(reason string) (service.GatewayBorrowPolicyResult, error) {
		return service.GatewayBorrowPolicyResult{Skipped: true, Reason: reason}, nil
	}
	client := clientFromContext(ctx, r.client)
	var raw string
	err := scanSingleRow(ctx, client, `SELECT jsonb_build_object(
 'credentials',credentials,'extra',extra,'proxy_id',proxy_id,
 'status',status,'schedulable',schedulable,'platform',platform,'type',type,
 'concurrency',concurrency,'priority',priority,'load_factor',load_factor,
 'group_ids',(SELECT COALESCE(jsonb_agg(group_id ORDER BY group_id),'[]'::jsonb) FROM account_groups WHERE account_id=$1),
 'parent_account_id',parent_account_id)::text
 FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, []any{id}, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return skipped("account_unavailable")
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
		Concurrency int            `json:"concurrency"`
		Priority    int            `json:"priority"`
		LoadFactor  *int           `json:"load_factor"`
		GroupIDs    []int64        `json:"group_ids"`
	}
	if err = json.Unmarshal([]byte(raw), &row); err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	if row.Platform != service.PlatformOpenAI || row.Type != service.AccountTypeOAuth || row.ParentID != nil {
		return skipped("account_unavailable")
	}
	if row.Status != service.StatusActive || !row.Schedulable {
		return skipped("paused_or_inactive")
	}
	if mode, _ := row.Extra[service.GatewayBorrowAccountQualityModeKey].(string); mode != "" && mode != o.PolicyMode {
		return skipped("policy_mode_changed")
	}
	if o.PolicyMode == service.GatewayBorrowAccountQualityMode && row.Extra[service.GatewayBorrowAccountQualityPendingKey] == true && row.Extra[service.GatewayBorrowInitialReadyKey] != true {
		return skipped("initial_defaults_not_ready")
	}
	if time.Since(o.ObservedAt) > 10*time.Minute {
		return skipped("stale_observation")
	}
	for _, r := range o.ModelResults {
		if r.State != "inconclusive" && time.Since(r.CheckedAt) > 10*time.Minute {
			return skipped("stale_model_result")
		}
	}
	expected := service.GatewayBorrowPolicyProjection(o.ExpectedPolicy)
	// Canonical JSON makes explicit null and absent owned keys equivalent.
	expectedRaw, err := json.Marshal(expected)
	if err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	var expectedNormalized map[string]any
	if err = json.Unmarshal(expectedRaw, &expectedNormalized); err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	if !reflect.DeepEqual(row.ProxyID, o.ExpectedProxyID) || service.GatewayBorrowCredentialSHA256(row.Credentials) != o.CredentialSHA256 || !reflect.DeepEqual(service.GatewayBorrowPolicyProjection(row.Extra), expectedNormalized) {
		return skipped("policy_or_identity_changed")
	}
	if token, ok := row.Credentials["access_token"].(string); !ok || token == "" {
		return skipped("account_unavailable")
	}
	if o.PolicyMode == service.GatewayBorrowAccountQualityMode && row.Extra[service.GatewayBorrowAccountQualityPendingKey] == true && !service.GatewayBorrowInitialReadyMatchesCurrent(&service.Account{Credentials: row.Credentials, Extra: row.Extra, ProxyID: row.ProxyID, Concurrency: row.Concurrency, Priority: row.Priority, LoadFactor: row.LoadFactor, GroupIDs: row.GroupIDs}) {
		return skipped("initial_configuration_changed")
	}
	updates, reason, err := service.BuildGatewayBorrowPolicyUpdates(row.Extra, o)
	if err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	if updates == nil && reason != "" {
		return skipped(reason)
	}
	same := true
	for key, value := range updates {
		b, e := json.Marshal(value)
		if e != nil {
			return service.GatewayBorrowPolicyResult{}, e
		}
		var normalized any
		if e = json.Unmarshal(b, &normalized); e != nil {
			return service.GatewayBorrowPolicyResult{}, e
		}
		if !reflect.DeepEqual(row.Extra[key], normalized) {
			same = false
		}
	}
	if same {
		return skipped("unchanged")
	}
	updateRaw, err := json.Marshal(updates)
	if err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	var changed int64
	err = scanSingleRow(ctx, client, `UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb)||$2::jsonb,updated_at=NOW() WHERE id=$1 RETURNING id`, []any{id, string(updateRaw)}, &changed)
	if err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	if err = enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return service.GatewayBorrowPolicyResult{}, err
	}
	if reason == "" {
		reason = "applied"
	}
	return service.GatewayBorrowPolicyResult{Applied: true, Reason: reason}, nil
}
