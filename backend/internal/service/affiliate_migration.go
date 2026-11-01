package service

import (
	"context"
	"errors"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrAffiliateInviterInvalid     = infraerrors.BadRequest("AFFILIATE_INVITER_INVALID", "invalid migration inviter")
	ErrAffiliateInviterUnavailable = infraerrors.BadRequest("AFFILIATE_INVITER_UNAVAILABLE", "migration inviter unavailable")
	ErrAffiliateBindingUnconfirmed = infraerrors.ServiceUnavailable("AFFILIATE_BINDING_UNCONFIRMED", "affiliate binding could not be confirmed")
)

// IDs belong to the verified migration catalog, never to caller input. In
// particular, hythyr is active user 52; soft-deleted duplicate 210 is excluded.
var migrationInviters = [...]struct {
	Key    string
	Label  string
	UserID int64
}{
	{"ritel", "**廷 / Ritel （站长）", 1},
	{"perry", "潘* / Perry / pR （图片站站长）", 127},
	{"ryverse", "杨* / 汽修二班cyh / 吳菸麵", 24},
	{"oilnight", "金** / OilNight33 / Rupture", 211},
	{"hythyr", "侯** / Hythyr", 52},
	{"joyce", "彭** / 蓬蒿人 / Joyce Peng", 190},
	{"alaric", "刘* / A2 / Shinoaki", 199},
	{"xenoge", "高** / 搞打折", 212},
}

type AffiliateMigrationOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type AffiliateMigration struct {
	Bound   bool                       `json:"bound"`
	Options []AffiliateMigrationOption `json:"options"`
}

func (s *AffiliateService) GetAffiliateMigration(ctx context.Context, userID int64) (*AffiliateMigration, error) {
	summary, err := s.EnsureUserAffiliate(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := &AffiliateMigration{Bound: summary.InviterID != nil, Options: []AffiliateMigrationOption{}}
	if !result.Bound {
		for _, inviter := range migrationInviters {
			if inviter.UserID != userID {
				result.Options = append(result.Options, AffiliateMigrationOption{Key: inviter.Key, Label: inviter.Label})
			}
		}
	}
	return result, nil
}

// BindMigrationInviter only attaches the caller's inviter. Registration policy,
// signup gifts and recharge/rebate processing must not run for migrated users.
// The handler supplies its existing UserService so target validation uses the
// same active/soft-deleted user view as the native application.
func (s *AffiliateService) BindMigrationInviter(ctx context.Context, userID int64, key string, users *UserService) (*AffiliateMigration, error) {
	summary, err := s.EnsureUserAffiliate(ctx, userID)
	if err != nil {
		return nil, err
	}
	if summary.InviterID != nil {
		return nil, ErrAffiliateAlreadyBound
	}
	var inviterID int64
	for _, inviter := range migrationInviters {
		if inviter.Key == key {
			inviterID = inviter.UserID
			break
		}
	}
	if inviterID == 0 || inviterID == userID {
		return nil, ErrAffiliateInviterInvalid
	}
	target, err := users.GetByID(ctx, inviterID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrAffiliateInviterUnavailable
		}
		return nil, err
	}
	if target == nil || !target.IsActive() || target.DeletedAt != nil {
		return nil, ErrAffiliateInviterUnavailable
	}
	inviter, err := s.repo.EnsureUserAffiliate(ctx, inviterID)
	if err != nil {
		return nil, err
	}
	// Resolve the current native code, including admin-customized codes; do not
	// freeze codes in the catalog or use the registration binding entry point.
	current, err := s.repo.GetAffiliateByCode(ctx, inviter.AffCode)
	if err != nil {
		if errors.Is(err, ErrAffiliateProfileNotFound) {
			return nil, ErrAffiliateInviterUnavailable
		}
		return nil, err
	}
	if current == nil || current.UserID != inviterID {
		return nil, ErrAffiliateInviterUnavailable
	}
	bound, err := s.repo.BindInviter(ctx, userID, inviterID)
	if err != nil {
		return nil, err
	}
	if !bound {
		return nil, ErrAffiliateAlreadyBound
	}
	persisted, err := s.repo.EnsureUserAffiliate(ctx, userID)
	if err != nil {
		return nil, err
	}
	if persisted == nil || persisted.InviterID == nil || *persisted.InviterID != inviterID {
		return nil, ErrAffiliateBindingUnconfirmed
	}
	return &AffiliateMigration{Bound: true, Options: []AffiliateMigrationOption{}}, nil
}
