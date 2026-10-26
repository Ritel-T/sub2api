package service

import "context"

// AccountFactorProof returns the original validated native authentication proof.
func AccountFactorProof(ctx context.Context) NativeAuthenticationProof {
	return nativeAuthenticationFromContext(ctx)
}

// AccountFactorGeneration reuses the native account generation algorithm.
func AccountFactorGeneration(user *User, epoch string) string {
	return accountCenterGeneration(user, epoch)
}

// AccountTotpAtomicEnabler fences both TOTP writes against the original proof.
// Legacy repositories need not implement this optional capability.
type AccountTotpAtomicEnabler interface {
	EnableTotpWithSecret(ctx context.Context, userID int64, encryptedSecret string, proof NativeAuthenticationProof) error
}
