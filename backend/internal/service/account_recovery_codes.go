package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	// AccountRecoveryCodeCount is the complete replacement set size.
	AccountRecoveryCodeCount = 8
	// AccountRecoveryCodeEntropyBytes gives every code 160 bits of random entropy.
	AccountRecoveryCodeEntropyBytes = 20
	accountRecoveryCodeCharacters   = AccountRecoveryCodeEntropyBytes * 8 / 5
)

var (
	ErrInvalidAccountRecoveryCode      = errors.New("invalid account recovery code")
	ErrInvalidAccountRecoveryCodeInput = errors.New("invalid account recovery code input")
	ErrAccountRecoveryCodeGeneration   = errors.New("account recovery code generation failed")
	ErrAccountRecoveryCodeStorage      = errors.New("account recovery code storage operation failed")
)

// AccountRecoveryCodeRepository stores only hashes against the original Sub user.
// The caller must authenticate/authorize the user and supply the security generation
// validated by that authentication. This repository does not authorize identities.
// Every operation serializes on the same per-user transaction lock, including
// operations against an empty set. A stale generation cannot consume a new set.
// Replace must receive exactly AccountRecoveryCodeCount distinct SHA-256 hashes.
// Invalidate is idempotent and removes all generations for the user.
// An uncertain commit returns an error; callers must not blindly retry consumption.
type AccountRecoveryCodeRepository interface {
	Replace(ctx context.Context, userID int64, generation string, hashes []string) error
	Consume(ctx context.Context, userID int64, generation string, hash string) (bool, error)
	Remaining(ctx context.Context, userID int64, generation string) (int, error)
	Invalidate(ctx context.Context, userID int64) error
}

// GenerateAccountRecoveryCodes returns display codes and their corresponding
// hashes. Only hashes may be persisted; display codes are for one-time delivery
// to the authenticated owner. There are no password or identity-store semantics.
func GenerateAccountRecoveryCodes() (codes []string, hashes []string, err error) {
	codes = make([]string, 0, AccountRecoveryCodeCount)
	hashes = make([]string, 0, AccountRecoveryCodeCount)
	seen := make(map[string]struct{}, AccountRecoveryCodeCount)
	encoding := base32.StdEncoding.WithPadding(base32.NoPadding)
	for range AccountRecoveryCodeCount {
		entropy := make([]byte, AccountRecoveryCodeEntropyBytes)
		if _, err := rand.Read(entropy); err != nil {
			return nil, nil, ErrAccountRecoveryCodeGeneration
		}
		compact := encoding.EncodeToString(entropy)
		groups := make([]string, 0, len(compact)/4)
		for start := 0; start < len(compact); start += 4 {
			groups = append(groups, compact[start:start+4])
		}
		code := strings.Join(groups, "-")
		hash, err := HashAccountRecoveryCode(code)
		if err != nil {
			return nil, nil, ErrAccountRecoveryCodeGeneration
		}
		if _, exists := seen[hash]; exists {
			return nil, nil, ErrAccountRecoveryCodeGeneration
		}
		seen[hash] = struct{}{}
		codes = append(codes, code)
		hashes = append(hashes, hash)
	}
	return codes, hashes, nil
}

// HashAccountRecoveryCode normalizes ASCII case, hyphens, and ASCII whitespace
// before domain-separated SHA-256 hashing. The full 32 base32 symbols are
// mandatory: no truncation, Unicode lookalikes, padding, or ambiguous aliases.
// High entropy permits a fast hash without storing an additional secret.
func HashAccountRecoveryCode(code string) (string, error) {
	if len(code) == 0 || len(code) > 128 {
		return "", ErrInvalidAccountRecoveryCode
	}
	var normalized [accountRecoveryCodeCharacters]byte
	count := 0
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch c {
		case '-', ' ', '\t', '\r', '\n':
			continue
		}
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if !((c >= 'A' && c <= 'Z') || (c >= '2' && c <= '7')) || count == len(normalized) {
			return "", ErrInvalidAccountRecoveryCode
		}
		normalized[count] = c
		count++
	}
	if count != len(normalized) {
		return "", ErrInvalidAccountRecoveryCode
	}
	sum := sha256.Sum256([]byte("sub2api:account-recovery-code:v1:" + string(normalized[:])))
	return hex.EncodeToString(sum[:]), nil
}
