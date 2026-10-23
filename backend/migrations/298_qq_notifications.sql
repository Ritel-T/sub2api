CREATE TABLE qq_user_notifications (
 user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 qq TEXT UNIQUE,
 channel TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email','qq','both')),
 balance_low BOOLEAN NOT NULL DEFAULT TRUE,
 subscription_expiry BOOLEAN NOT NULL DEFAULT TRUE,
 code_hash TEXT UNIQUE,
 pending_qq TEXT,
 expires_at TIMESTAMPTZ,
 issued_at TIMESTAMPTZ,
 CHECK (channel = 'email' OR qq IS NOT NULL)
);
CREATE TABLE qq_delivery_attempts (
 delivery_key TEXT PRIMARY KEY,
 user_id BIGINT,
 qq TEXT NOT NULL,
 state TEXT NOT NULL CHECK (state IN ('attempting','sent','unknown','rejected')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 message_id TEXT,
 finished_at TIMESTAMPTZ
);
CREATE INDEX qq_delivery_attempts_created ON qq_delivery_attempts(created_at);
CREATE TABLE qq_inbound_events (
 event_key TEXT PRIMARY KEY,
 sender TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX qq_inbound_sender_time ON qq_inbound_events(sender,created_at);
