package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type disabledAccountOpsGroupReader struct {
	AccountRepository
	calls int
}

func (r *disabledAccountOpsGroupReader) ListAllWithFilters(context.Context, string, string, string, string, int64, string) ([]Account, error) {
	r.calls++
	return []Account{{ID: 17, Name: "existing", Platform: PlatformOpenAI, Type: AccountTypeOAuth}}, nil
}

func TestDisabledAccountOpsRefreshDoesNotInitializeOrPersistGroups(t *testing.T) {
	ctx := context.Background()
	settings := &accountOpsSettingsStub{raw: `{"enabled":false}`}
	reader := &disabledAccountOpsGroupReader{}
	svc := NewAccountOpsService(settings, &accountOpsRepoStub{}, &accountOpsSenderStub{})
	svc.accounts = reader
	require.False(t, svc.refreshConfig(ctx))
	require.Zero(t, reader.calls, "disabled background refresh should not discover groups")
	require.Equal(t, `{"enabled":false}`, settings.raw)
	require.False(t, svc.currentConfig().GroupsInitialized)

	// Cover the race where refresh initially saw enabled, but the settings
	// re-read inside reconciliation observes disabled.
	config, err := svc.ensureManualAccountOpsGroups(ctx)
	require.NoError(t, err)
	require.False(t, config.Enabled)
	require.False(t, config.GroupsInitialized)
	require.Equal(t, `{"enabled":false}`, settings.raw, "reconciliation must not write after disable")
}
