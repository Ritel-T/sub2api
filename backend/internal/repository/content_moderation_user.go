package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const moderationUserProjection = `SELECT u.id, COALESCE(s.version,0), COALESCE(s.generation,0),
 COALESCE(s.auto_ban_exempt,FALSE),
 COALESCE(u.status='disabled' AND s.owned_status_revision=u.moderation_status_revision,FALSE),
 u.status, (SELECT COUNT(*) FROM content_moderation_user_hits h WHERE h.user_id=u.id
 AND h.generation=COALESCE(s.generation,0) AND h.created_at >= $1
 AND ($2::bool=FALSE OR h.action<>'cyber_policy')),
 COALESCE(s.reason,''),s.actor_id,s.updated_at
 FROM users u LEFT JOIN content_moderation_user_state s ON s.user_id=u.id `

type moderationScanner interface{ Scan(...any) error }

func scanModerationUser(row moderationScanner) (*service.ModerationUserState, error) {
	s := new(service.ModerationUserState)
	err := row.Scan(&s.UserID, &s.Version, &s.Generation, &s.AutoBanExempt, &s.OwnedDisable, &s.Status, &s.CurrentCount, &s.Reason, &s.ActorID, &s.UpdatedAt)
	return s, err
}

func (r *contentModerationRepository) GetModerationUser(ctx context.Context, id int64, since time.Time, exclude bool) (*service.ModerationUserState, error) {
	s, err := scanModerationUser(r.db.QueryRowContext(ctx, moderationUserProjection+`WHERE u.id=$3 AND u.deleted_at IS NULL`, since, exclude, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	}
	return s, err
}

func (r *contentModerationRepository) ListModerationUsers(ctx context.Context, page, size int, state string, userID int64, since time.Time, exclude bool) ([]service.ModerationUserState, int64, error) {
	where := `WHERE u.deleted_at IS NULL AND s.user_id IS NOT NULL AND ($3='all' OR ($3='exempt' AND s.auto_ban_exempt) OR ($3='enforced' AND NOT s.auto_ban_exempt)) AND ($4::bigint=0 OR u.id=$4)`
	var total int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u JOIN content_moderation_user_state s ON s.user_id=u.id `+`WHERE u.deleted_at IS NULL AND ($1='all' OR ($1='exempt' AND s.auto_ban_exempt) OR ($1='enforced' AND NOT s.auto_ban_exempt)) AND ($2::bigint=0 OR u.id=$2)`, state, userID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, moderationUserProjection+where+` ORDER BY s.updated_at DESC,u.id DESC LIMIT $5 OFFSET $6`, since, exclude, state, userID, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.ModerationUserState, 0)
	for rows.Next() {
		s, err := scanModerationUser(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *s)
	}
	return items, total, rows.Err()
}

type lockedModerationUser struct {
	status, role                  string
	revision, version, generation int64
	exempt                        bool
	owned                         sql.NullInt64
}

func lockModerationUser(ctx context.Context, tx *sql.Tx, id int64) (lockedModerationUser, error) {
	var u lockedModerationUser
	err := tx.QueryRowContext(ctx, `SELECT status, role, moderation_status_revision FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&u.status, &u.role, &u.revision)
	if errors.Is(err, sql.ErrNoRows) {
		return u, service.ErrUserNotFound
	}
	if err != nil {
		return u, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO content_moderation_user_state(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, id)
	if err != nil {
		return u, err
	}
	err = tx.QueryRowContext(ctx, `SELECT version, generation, auto_ban_exempt, owned_status_revision FROM content_moderation_user_state WHERE user_id=$1 FOR UPDATE`, id).Scan(&u.version, &u.generation, &u.exempt, &u.owned)
	return u, err
}

func (r *contentModerationRepository) ManageModerationUser(ctx context.Context, id int64, in service.ModerationUserAction) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	u, err := lockModerationUser(ctx, tx, id)
	if err != nil {
		return err
	}
	if u.version != in.Version {
		return infraerrors.Conflict("MODERATION_STATE_CONFLICT", "State changed; reload before retrying")
	}
	switch in.Action {
	case "unban":
		if u.status == service.StatusDisabled {
			if !u.owned.Valid || u.owned.Int64 != u.revision {
				return infraerrors.Conflict("MODERATION_DISABLE_NOT_OWNED", "MODERATION_DISABLE_NOT_OWNED: current disable is not owned by content moderation; review its source separately")
			}
			var status string
			if err = tx.QueryRowContext(ctx, `UPDATE users SET status='active',updated_at=NOW() WHERE id=$1 AND status='disabled' AND moderation_status_revision=$2 RETURNING status`, id, u.revision).Scan(&status); err != nil {
				return err
			}
			if status != service.StatusActive {
				return fmt.Errorf("moderation unban readback: unexpected status")
			}
		} else if u.status != service.StatusActive {
			return infraerrors.Conflict("MODERATION_DISABLE_NOT_OWNED", "Current state is not a content moderation disable")
		}
		u.owned = sql.NullInt64{}
	case "reset":
		u.generation++
	case "exemption":
		u.exempt = *in.Exempt
	default:
		return fmt.Errorf("invalid moderation action")
	}
	var owned any
	if u.owned.Valid {
		owned = u.owned.Int64
	}
	_, err = tx.ExecContext(ctx, `UPDATE content_moderation_user_state SET version=version+1,generation=$2,auto_ban_exempt=$3,owned_status_revision=$4,reason=$5,actor_id=$6,updated_at=NOW() WHERE user_id=$1`, id, u.generation, u.exempt, owned, in.Reason, in.ActorID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO content_moderation_user_actions(user_id,actor_id,action,reason,version,auto_ban_exempt) VALUES($1,$2,$3,$4,$5,$6)`, id, in.ActorID, in.Action, in.Reason, u.version+1, u.exempt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *contentModerationRepository) ApplyModerationHit(ctx context.Context, in service.ModerationHit) (service.ModerationHitResult, error) {
	var result service.ModerationHitResult
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	u, err := lockModerationUser(ctx, tx, in.UserID)
	if err != nil {
		return result, err
	}
	if in.Version < 0 || u.version != in.Version {
		return result, nil
	}
	inserted, err := tx.ExecContext(ctx, `INSERT INTO content_moderation_user_hits(user_id,generation,request_id,action) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,generation,request_id) DO NOTHING`, in.UserID, u.generation, in.RequestID, in.Action)
	if err != nil {
		return result, err
	}
	n, err := inserted.RowsAffected()
	if err != nil {
		return result, err
	}
	if n == 0 {
		return result, nil
	}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_moderation_user_hits WHERE user_id=$1 AND generation=$2 AND created_at >= $3 AND ($4::bool=FALSE OR action<>'cyber_policy')`, in.UserID, u.generation, in.Since, in.ExcludeCyber).Scan(&result.Count)
	if err != nil {
		return result, err
	}
	if in.AutoBan && in.Threshold > 0 && result.Count >= in.Threshold && !u.exempt && u.role != service.RoleAdmin && u.status == service.StatusActive {
		var revision int64
		if err = tx.QueryRowContext(ctx, `UPDATE users SET status='disabled',updated_at=NOW() WHERE id=$1 AND status='active' RETURNING moderation_status_revision`, in.UserID).Scan(&revision); err != nil {
			return result, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE content_moderation_user_state SET owned_status_revision=$2,version=version+1,updated_at=NOW() WHERE user_id=$1`, in.UserID, revision)
		if err != nil {
			return result, err
		}
		result.BanApplied = true
		result.OwnedDisable = true
	} else {
		result.OwnedDisable = u.status == service.StatusDisabled && u.owned.Valid && u.owned.Int64 == u.revision
	}
	if err = tx.Commit(); err != nil {
		return service.ModerationHitResult{}, err
	}
	result.Accepted = true
	return result, nil
}
