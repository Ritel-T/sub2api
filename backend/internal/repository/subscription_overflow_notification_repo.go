package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

type subscriptionOverflowNotificationRepository struct{ db *sql.DB }

func NewSubscriptionOverflowNotificationRepository(db *sql.DB) service.SubscriptionOverflowNotificationRepository {
	return &subscriptionOverflowNotificationRepository{db: db}
}

func enqueueSubscriptionOverflowNotification(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	e := cmd.SubscriptionOverflow
	if !e.Valid() || cmd.UserID <= 0 || cmd.SubscriptionID != nil || cmd.BalanceCost <= 0 || result.NewBalance == nil {
		return nil
	}
	payload, err := json.Marshal(service.SubscriptionOverflowNotificationPayload{
		SubscriptionOverflowBillingEvidence: *e, UserID: cmd.UserID,
		BalanceCost: cmd.BalanceCost, BalanceAfter: *result.NewBalance, ChargedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO subscription_overflow_email_outbox (user_id,subscription_id,dimension,window_start,payload)
		SELECT $1,$2,$3,$4,$5::jsonb || jsonb_build_object(
			'recipient_email',u.email,'recipient_name',COALESCE(u.username,''),
			'source_group_name',COALESCE(src.name,''),'target_group_name',COALESCE(dst.name,''))
		FROM users u
		LEFT JOIN groups src ON src.id=$6
		LEFT JOIN groups dst ON dst.id=$7
		WHERE u.id=$1 AND u.deleted_at IS NULL
		ON CONFLICT (user_id,subscription_id,dimension,window_start) DO NOTHING
	`, cmd.UserID, e.SubscriptionID, e.Dimension, e.WindowStart.UTC(), string(payload), e.SourceGroupID, e.TargetGroupID)
	return err
}

func (r *subscriptionOverflowNotificationRepository) Claim(ctx context.Context, now time.Time) (*service.SubscriptionOverflowNotification, error) {
	// A process can die after SMTP acceptance and before marking sent. Do not
	// recycle expired sending leases: unknown delivery needs manual reconciliation.
	if _, err := r.db.ExecContext(ctx, `
		UPDATE subscription_overflow_email_outbox
		SET status='unknown',last_error_code='claim_expired',completed_at=$1,updated_at=$1
		WHERE status='sending' AND claimed_at < $2
	`, now, now.Add(-10*time.Minute)); err != nil {
		return nil, err
	}
	job := &service.SubscriptionOverflowNotification{ClaimToken: uuid.NewString()}
	var payload []byte
	err := r.db.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT id FROM subscription_overflow_email_outbox
			WHERE status='pending' AND next_attempt_at <= $1 AND attempts < 3
			ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT 1
		)
		UPDATE subscription_overflow_email_outbox o
		SET status='sending',claim_token=$2,claimed_at=$1,attempts=o.attempts+1,updated_at=$1
		FROM candidate c WHERE o.id=c.id
		RETURNING o.id,o.attempts,o.payload
	`, now, job.ClaimToken).Scan(&job.ID, &job.Attempts, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(payload, &job.Payload); err != nil {
		if completeErr := r.Complete(ctx, job.ID, job.ClaimToken, "failed", "invalid_payload", time.Time{}); completeErr != nil {
			return nil, completeErr
		}
		return nil, err
	}
	return job, nil
}

func (r *subscriptionOverflowNotificationRepository) Complete(ctx context.Context, id int64, token, state, code string, next time.Time) error {
	switch state {
	case "pending", "sent", "failed", "unknown", "suppressed":
	default:
		return errors.New("invalid overflow notification completion state")
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE subscription_overflow_email_outbox
		SET status=$3,last_error_code=NULLIF($4,''),next_attempt_at=COALESCE($5,next_attempt_at),
			completed_at=CASE WHEN $3='pending' THEN NULL ELSE NOW() END,updated_at=NOW()
		WHERE id=$1 AND claim_token=$2 AND status='sending'
	`, id, token, state, code, nullableOverflowNextAttempt(next))
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("overflow notification claim no longer owned")
	}
	return nil
}

func nullableOverflowNextAttempt(next time.Time) any {
	if next.IsZero() {
		return nil
	}
	return next
}
