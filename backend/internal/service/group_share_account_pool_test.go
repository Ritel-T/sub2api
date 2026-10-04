package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

type poolShareRepo struct {
	GroupRepository
	calls int
}

func (r *poolShareRepo) ShareAccountPoolIfObserved(context.Context, int64, ShareAccountPoolInput) (ShareAccountPoolResult, error) {
	r.calls++
	return ShareAccountPoolResult{Applied: true}, nil
}
func TestShareAccountPoolInput(t *testing.T) {
	valid := ShareAccountPoolInput{SourceGroupID: 1, ExpectedSourceAccountIDs: []int64{1, 2}, ExpectedTargetAccountIDs: []int64{1}}
	require.NoError(t, ValidateShareAccountPoolInput(2, valid))
	for _, mutate := range []func(*ShareAccountPoolInput){func(i *ShareAccountPoolInput) { i.SourceGroupID = 2 }, func(i *ShareAccountPoolInput) { i.ExpectedTargetAccountIDs = nil }, func(i *ShareAccountPoolInput) { i.ExpectedSourceAccountIDs = []int64{1, 1} }, func(i *ShareAccountPoolInput) { i.ExpectedTargetAccountIDs = []int64{3} }} {
		i := valid
		mutate(&i)
		require.Error(t, ValidateShareAccountPoolInput(2, i))
	}
	repo := &poolShareRepo{}
	s := &adminServiceImpl{groupRepo: repo}
	result, err := s.ShareAccountPool(context.Background(), 2, valid)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Equal(t, 1, repo.calls)
}
