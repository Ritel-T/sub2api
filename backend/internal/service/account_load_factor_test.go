//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func intPtrHelper(v int) *int { return &v }

func TestEffectiveLoadFactor_PositiveLoadFactor(t *testing.T) {
	a := &Account{Concurrency: 5, LoadFactor: intPtrHelper(20)}
	require.Equal(t, 20, a.EffectiveLoadFactor())
}

func TestAccountLoadPreferenceRateIsBoundedAndKeepsCapacitySeparate(t *testing.T) {
	highWeight := &Account{Concurrency: 2, LoadFactor: intPtrHelper(10000)}
	lowWeight := &Account{Concurrency: 8, LoadFactor: intPtrHelper(1)}

	require.Equal(t, 25, accountLoadPreferenceRate(highWeight, 50))
	require.Equal(t, 100, accountLoadPreferenceRate(lowWeight, 50))
	require.Equal(t, 50, accountLoadPreferenceRate(&Account{Concurrency: 2}, 50))
	require.Equal(t, 2, highWeight.Concurrency, "load_factor must not change slot capacity")
	require.Equal(t, 50, accountLoadPreferenceRate(&Account{Concurrency: 0, LoadFactor: intPtrHelper(10000)}, 50), "unlimited capacity keeps load preference neutral")
}
