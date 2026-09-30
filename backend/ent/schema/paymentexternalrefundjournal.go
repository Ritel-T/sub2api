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

// PaymentExternalRefundJournal stores only payment identifiers, monetary proof and recovery state; no customer PII.
type PaymentExternalRefundJournal struct{ ent.Schema }

func (PaymentExternalRefundJournal) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "payment_external_refund_journals"}}
}
func (PaymentExternalRefundJournal) Fields() []ent.Field {
	return []ent.Field{

		field.Int64("external_order_ledger_id"),
		field.Int64("cumulative_refund_minor"),
		field.Int64("delta_refund_minor"),
		field.Int64("target_usd_units"),
		field.Int64("delta_target_usd_units"),
		field.Int64("recovered_usd_units").Default(0),
		field.Int64("debt_usd_units").Default(0),
		field.String("status").MaxLen(30).Default("RECORDED"),

		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}
func (PaymentExternalRefundJournal) Indexes() []ent.Index {
	return []ent.Index{

		index.Fields("external_order_ledger_id", "cumulative_refund_minor").Unique().StorageKey("paymentexternalrefundjournal_order_cumulative"),
		index.Fields("status"),
	}
}
