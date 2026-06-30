// Package schema 定义 Ent ORM 的数据库 schema。
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UsageRequestCaptureShare 定义请求捕获公开分享实体的 schema。
type UsageRequestCaptureShare struct {
	ent.Schema
}

// Annotations 返回 schema 的注解配置。
func (UsageRequestCaptureShare) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "usage_request_capture_shares"},
	}
}

// Fields 定义捕获分享实体的所有字段。
func (UsageRequestCaptureShare) Fields() []ent.Field {
	return []ent.Field{
		field.String("share_id").
			MaxLen(64).
			NotEmpty(),
		field.String("request_id").
			MaxLen(64).
			NotEmpty(),
		field.Int64("api_key_id").
			Optional().
			Nillable(),
		field.Int64("created_by").
			Optional().
			Nillable(),
		field.String("label").
			MaxLen(255).
			Optional().
			Nillable(),
		field.Time("expires_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("revoked_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int("view_count").
			Default(0),
		field.Time("last_viewed_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

// Indexes 定义数据库索引，优化查询性能。
func (UsageRequestCaptureShare) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("share_id").Unique(),
		index.Fields("request_id"),
		index.Fields("expires_at"),
	}
}
