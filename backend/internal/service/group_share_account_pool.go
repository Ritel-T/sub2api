package service

import (
	"context"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"net/http"
	"slices"
	"time"
)

type ShareAccountPoolInput struct {
	SourceGroupID            int64   `json:"source_group_id"`
	ExpectedSourceAccountIDs []int64 `json:"expected_source_account_ids"`
	ExpectedTargetAccountIDs []int64 `json:"expected_target_account_ids"`
}
type ShareAccountPoolResult struct {
	Applied         bool    `json:"applied"`
	Skipped         bool    `json:"skipped"`
	Reason          string  `json:"reason"`
	AddedAccountIDs []int64 `json:"added_account_ids"`
	TotalAccounts   int     `json:"total_accounts"`
}
type ShareAccountPoolRepository interface {
	ShareAccountPoolIfObserved(context.Context, int64, ShareAccountPoolInput) (ShareAccountPoolResult, error)
}
type AccountPoolSharingAdmin interface {
	ShareAccountPool(context.Context, int64, ShareAccountPoolInput) (ShareAccountPoolResult, error)
}

func ValidateShareAccountPoolInput(target int64, input ShareAccountPoolInput) error {
	if target <= 0 || input.SourceGroupID <= 0 || target == input.SourceGroupID || input.ExpectedSourceAccountIDs == nil || input.ExpectedTargetAccountIDs == nil || len(input.ExpectedSourceAccountIDs) == 0 || len(input.ExpectedSourceAccountIDs) > 10000 || len(input.ExpectedTargetAccountIDs) > 10000 {
		return infraerrors.New(http.StatusBadRequest, "INVALID_SHARE_ACCOUNT_POOL", "Invalid account pool observation")
	}
	for _, ids := range [][]int64{input.ExpectedSourceAccountIDs, input.ExpectedTargetAccountIDs} {
		sorted := slices.Clone(ids)
		slices.Sort(sorted)
		for i, id := range sorted {
			if id <= 0 || (i > 0 && sorted[i-1] == id) {
				return infraerrors.New(http.StatusBadRequest, "INVALID_SHARE_ACCOUNT_POOL", "Invalid account pool observation")
			}
		}
	}
	for _, id := range input.ExpectedTargetAccountIDs {
		if !slices.Contains(input.ExpectedSourceAccountIDs, id) {
			return infraerrors.New(http.StatusBadRequest, "TARGET_ACCOUNT_OUTSIDE_SOURCE", "Target account pool must be a subset of the source")
		}
	}
	return nil
}
func (s *adminServiceImpl) ShareAccountPool(ctx context.Context, target int64, input ShareAccountPoolInput) (ShareAccountPoolResult, error) {
	if err := ValidateShareAccountPoolInput(target, input); err != nil {
		return ShareAccountPoolResult{}, err
	}
	repo, ok := s.groupRepo.(ShareAccountPoolRepository)
	if !ok {
		return ShareAccountPoolResult{}, infraerrors.New(http.StatusServiceUnavailable, "ACCOUNT_POOL_SHARE_UNAVAILABLE", "Account pool sharing unavailable")
	}
	bounded, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	return repo.ShareAccountPoolIfObserved(bounded, target, input)
}
