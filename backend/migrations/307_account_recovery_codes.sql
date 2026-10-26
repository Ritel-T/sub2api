-- Sub-owned recovery factors for the existing users; no passwords or second user store.
-- Application operations share a per-user advisory transaction lock even when no
-- rows exist. The migration runner owns the enclosing transaction.
CREATE TABLE account_recovery_codes (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL CHECK (code_hash ~ '^[0-9a-f]{64}$'),
    generation TEXT NOT NULL CHECK (
        octet_length(generation) BETWEEN 1 AND 128 AND generation ~ '^[!-~]+$'
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    used_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, code_hash)
);

COMMENT ON TABLE account_recovery_codes IS
    'One-use recovery code hashes for original Sub users, bound to the authenticated security generation';
COMMENT ON COLUMN account_recovery_codes.code_hash IS
    'Domain-separated SHA-256 of a normalized high-entropy recovery code; never plaintext';
