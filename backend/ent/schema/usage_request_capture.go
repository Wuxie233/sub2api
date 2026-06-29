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

// UsageRequestCapture 定义网关请求/响应捕获实体的 schema。
type UsageRequestCapture struct {
	ent.Schema
}

// Annotations 返回 schema 的注解配置。
func (UsageRequestCapture) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "usage_request_captures"},
	}
}

// Fields 定义请求捕获实体的所有字段。
func (UsageRequestCapture) Fields() []ent.Field {
	return []ent.Field{
		// 关联字段
		field.String("request_id").
			MaxLen(64).
			NotEmpty(),
		field.Int64("api_key_id").
			Optional().
			Nillable(),
		field.Int64("usage_log_id").
			Optional().
			Nillable(),
		field.Int64("user_id").
			Optional().
			Nillable(),
		field.Int64("account_id").
			Optional().
			Nillable(),

		// 请求/响应元数据
		field.String("provider").
			MaxLen(50).
			NotEmpty(),
		field.String("model").
			MaxLen(100).
			NotEmpty(),
		field.String("endpoint").
			MaxLen(128).
			NotEmpty(),
		field.Bool("stream").
			Default(false),
		field.Int("status_code"),
		field.Int64("duration_ms"),
		field.Int64("request_bytes"),
		field.Int64("response_bytes"),
		field.Int64("compressed_bytes"),
		field.Bool("truncated").
			Default(false),
		field.String("truncate_reason").
			MaxLen(255).
			Optional().
			Nillable(),
		field.Int("capture_schema_version").
			Default(1),
		field.Bytes("payload_gzip").
			SchemaType(map[string]string{dialect.Postgres: "bytea"}),

		// 时间戳
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("expires_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

// Indexes 定义数据库索引，优化查询性能。
func (UsageRequestCapture) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_id"),
		index.Fields("request_id", "api_key_id").Unique(),
		index.Fields("expires_at"),
		index.Fields("user_id"),
	}
}
