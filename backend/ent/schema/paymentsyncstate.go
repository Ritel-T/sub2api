package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"time"
)

// PaymentSyncState stores only payment identifiers, monetary proof and recovery state; no customer PII.
type PaymentSyncState struct{ ent.Schema }

func (PaymentSyncState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "payment_sync_states"}}
}
func (PaymentSyncState) Fields() []ent.Field {
	return []ent.Field{

		field.String("provider_key").MaxLen(30).NotEmpty(),
		field.String("website_id").MaxLen(128).NotEmpty(),
		field.String("oauth_client_id").MaxLen(200).Default(""),
		field.String("encrypted_oauth_credentials").Sensitive().Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("encrypted_oauth_tokens").Sensitive().Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int64("token_version").Default(0),
		field.String("rotation_phase").MaxLen(30).Default("idle"),
		field.Time("rotation_started_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("next_cursor").Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("window_start").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("window_end").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_synced_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("retry_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("last_error_code").MaxLen(64).Default(""),
		field.String("lease_owner").MaxLen(64).Default(""),
		field.Time("lease_until").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),

		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}
func (PaymentSyncState) Indexes() []ent.Index {
	return []ent.Index{

		index.Fields("provider_key", "website_id").Unique(),
	}
}
