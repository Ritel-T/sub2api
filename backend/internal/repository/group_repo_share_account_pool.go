package repository

import (
	"context"
	"encoding/json"
	"errors"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"slices"
)

func (r *groupRepository) ShareAccountPoolIfObserved(ctx context.Context, target int64, input service.ShareAccountPoolInput) (service.ShareAccountPoolResult, error) {
	if err := service.ValidateShareAccountPoolInput(target, input); err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	if dbent.TxFromContext(ctx) != nil {
		return r.shareAccountPoolInTx(ctx, target, input)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return r.shareAccountPoolInTx(ctx, target, input)
	}
	if err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := r.shareAccountPoolInTx(dbent.NewTxContext(ctx, tx), target, input)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	return result, nil
}
func (r *groupRepository) shareAccountPoolInTx(ctx context.Context, target int64, input service.ShareAccountPoolInput) (service.ShareAccountPoolResult, error) {
	skip := func(reason string) (service.ShareAccountPoolResult, error) {
		return service.ShareAccountPoolResult{Skipped: true, Reason: reason, AddedAccountIDs: []int64{}}, nil
	}
	client := clientFromContext(ctx, r.client)
	// Binding writers lock groups FOR SHARE. Lock both in ID order, then lock
	// existing bindings, so neither concurrent inserts nor binding edits evade CAS.
	rows, err := client.QueryContext(ctx, `SELECT id,platform,status FROM groups WHERE id=ANY($1) AND deleted_at IS NULL ORDER BY id FOR UPDATE`, pq.Array([]int64{target, input.SourceGroupID}))
	if err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	groups := map[int64]string{}
	active := true
	for rows.Next() {
		var id int64
		var platform, status string
		if err = rows.Scan(&id, &platform, &status); err != nil {
			_ = rows.Close()
			return service.ShareAccountPoolResult{}, err
		}
		groups[id] = platform
		active = active && status == service.StatusActive
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	if len(groups) != 2 || !active {
		return skip("group_unavailable")
	}
	if groups[target] != groups[input.SourceGroupID] || groups[target] != service.PlatformOpenAI {
		return skip("group_platform_mismatch")
	}
	var raw string
	err = scanSingleRow(ctx, client, `SELECT COALESCE(jsonb_agg(jsonb_build_object('account_id',account_id,'group_id',group_id)),'[]'::jsonb)::text FROM (SELECT account_id,group_id FROM account_groups WHERE group_id=ANY($1) ORDER BY group_id,account_id FOR UPDATE) b`, []any{pq.Array([]int64{target, input.SourceGroupID})}, &raw)
	if err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	var bindings []struct {
		AccountID int64 `json:"account_id"`
		GroupID   int64 `json:"group_id"`
	}
	if err = json.Unmarshal([]byte(raw), &bindings); err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	sourceIDs, targetIDs := []int64{}, []int64{}
	for _, b := range bindings {
		if b.GroupID == target {
			targetIDs = append(targetIDs, b.AccountID)
		} else {
			sourceIDs = append(sourceIDs, b.AccountID)
		}
	}
	slices.Sort(sourceIDs)
	slices.Sort(targetIDs)
	sourceExpected, targetExpected := slices.Clone(input.ExpectedSourceAccountIDs), slices.Clone(input.ExpectedTargetAccountIDs)
	slices.Sort(sourceExpected)
	slices.Sort(targetExpected)
	if !slices.Equal(sourceIDs, sourceExpected) || !slices.Equal(targetIDs, targetExpected) {
		return skip("account_pool_changed")
	}
	// Lock all source accounts to reject deleted/cross-platform rows and
	// prevent identity/deletion changes before the missing bindings are inserted.
	rows, err = client.QueryContext(ctx, `SELECT id,platform,status,deleted_at IS NULL FROM accounts WHERE id=ANY($1) ORDER BY id FOR SHARE`, pq.Array(sourceIDs))
	if err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	valid, count := true, 0
	for rows.Next() {
		var id int64
		var platform, status string
		var live bool
		if err = rows.Scan(&id, &platform, &status, &live); err != nil {
			_ = rows.Close()
			return service.ShareAccountPoolResult{}, err
		}
		count++
		// Binding an inactive account does not enable it: its existing scheduler
		// status and pause gates remain authoritative, including after revival.
		valid = valid && live && platform == groups[target]
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	if !valid || count != len(sourceIDs) {
		return skip("account_unavailable")
	}
	added := []int64{}
	rows, err = client.QueryContext(ctx, `INSERT INTO account_groups (account_id,group_id,priority,allowed_models,created_at)
 SELECT s.account_id,$2,s.priority,s.allowed_models,NOW() FROM account_groups s
 WHERE s.group_id=$1 AND NOT EXISTS(SELECT 1 FROM account_groups t WHERE t.group_id=$2 AND t.account_id=s.account_id)
 ON CONFLICT(account_id,group_id) DO NOTHING RETURNING account_id`, input.SourceGroupID, target)
	if err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return service.ShareAccountPoolResult{}, err
		}
		added = append(added, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	slices.Sort(added)
	if len(added) == 0 {
		return service.ShareAccountPoolResult{Skipped: true, Reason: "already_shared", AddedAccountIDs: added, TotalAccounts: len(sourceIDs)}, nil
	}
	for _, id := range added {
		if err = enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			return service.ShareAccountPoolResult{}, err
		}
	}
	if err = enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventGroupChanged, nil, &target, nil); err != nil {
		return service.ShareAccountPoolResult{}, err
	}
	return service.ShareAccountPoolResult{Applied: true, Reason: "applied", AddedAccountIDs: added, TotalAccounts: len(sourceIDs)}, nil
}
