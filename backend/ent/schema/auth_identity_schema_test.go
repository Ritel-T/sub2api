package schema

import (
	"testing"

	"entgo.io/ent"
	"github.com/stretchr/testify/require"
)

func TestUserSignupSourceValidator(t *testing.T) {
	validator := requireStringFieldValidator(t, User{}.Fields(), "signup_source")
	for _, value := range []string{"email", "linuxdo", "wechat", "oidc", "github", "google", "dingtalk"} {
		require.NoError(t, validator(value))
	}
	require.Error(t, validator("unknown"))
}

func requireStringFieldValidator(t *testing.T, fields []ent.Field, name string) func(string) error {
	t.Helper()

	for _, entField := range fields {
		descriptor := entField.Descriptor()
		if descriptor.Name != name {
			continue
		}
		require.NotEmpty(t, descriptor.Validators, "field %s should include a validator", name)
		validator, ok := descriptor.Validators[0].(func(string) error)
		require.True(t, ok, "field %s validator should be func(string) error", name)
		return validator
	}

	require.Failf(t, "missing field validator", "schema should include field %s", name)
	return nil
}
