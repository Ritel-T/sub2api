package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/setting"
)

// ClaimBalanceEmailAttempt relies on the existing unique settings.key index.
// Never upsert here: replacing a peer's owner would permit duplicate SMTP sends.
func (r *settingRepository) ClaimBalanceEmailAttempt(ctx context.Context, key, owner string) (bool, error) {
	_, err := r.client.Setting.Create().SetKey(key).SetValue(owner).SetUpdatedAt(time.Now()).Save(ctx)
	if ent.IsConstraintError(err) {
		return false, nil
	}
	return err == nil, err
}

// ReleaseBalanceEmailAttempt is used only after certified pre-DATA failures.
// Match both key and owner so a late completion cannot release a peer's claim.
func (r *settingRepository) ReleaseBalanceEmailAttempt(ctx context.Context, key, owner string) (bool, error) {
	n, err := r.client.Setting.Delete().Where(setting.KeyEQ(key), setting.ValueEQ(owner)).Exec(ctx)
	return n == 1, err
}
