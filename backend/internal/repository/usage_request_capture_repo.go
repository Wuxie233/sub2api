package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const usageRequestCaptureSelectColumns = "id, request_id, api_key_id, usage_log_id, user_id, account_id, provider, model, endpoint, stream, status_code, duration_ms, request_bytes, response_bytes, compressed_bytes, truncated, truncate_reason, capture_schema_version, payload_gzip, created_at, expires_at"

type usageRequestCaptureRepository struct {
	client *dbent.Client
	sql    sqlExecutor
}

type usageRequestCaptureInsertPrepared struct {
	requestID string
	apiKeyID  sql.NullInt64
	createdAt time.Time
	args      []any
}

func NewUsageRequestCaptureRepository(client *dbent.Client, sqlDB *sql.DB) service.UsageRequestCaptureRepository {
	return newUsageRequestCaptureRepositoryWithSQL(client, sqlDB)
}

func newUsageRequestCaptureRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor) *usageRequestCaptureRepository {
	return &usageRequestCaptureRepository{client: client, sql: sqlq}
}

func (r *usageRequestCaptureRepository) CreateBestEffort(ctx context.Context, capture *service.UsageRequestCapture) error {
	if capture == nil {
		return nil
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.createSingle(ctx, tx.Client(), capture)
	}
	return r.createSingle(ctx, r.sql, capture)
}

func (r *usageRequestCaptureRepository) createSingle(ctx context.Context, sqlq sqlExecutor, capture *service.UsageRequestCapture) error {
	if sqlq == nil {
		sqlq = r.sql
	}
	prepared := prepareUsageRequestCaptureInsert(capture)
	query := `
		INSERT INTO usage_request_captures (
			request_id,
			api_key_id,
			usage_log_id,
			user_id,
			account_id,
			provider,
			model,
			endpoint,
			stream,
			status_code,
			duration_ms,
			request_bytes,
			response_bytes,
			compressed_bytes,
			truncated,
			truncate_reason,
			capture_schema_version,
			payload_gzip,
			created_at,
			expires_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19, $20
		)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id, created_at
	`
	if err := scanSingleRow(ctx, sqlq, query, prepared.args, &capture.ID, &capture.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) && prepared.requestID != "" {
			selectQuery, args := buildUsageRequestCaptureKeyQuery("SELECT id, created_at FROM usage_request_captures WHERE request_id = $1", prepared.requestID, capture.APIKeyID)
			if err := scanSingleRow(ctx, sqlq, selectQuery, args, &capture.ID, &capture.CreatedAt); err != nil {
				return err
			}
			return nil
		}
		return err
	}
	return nil
}

func (r *usageRequestCaptureRepository) GetByRequestID(ctx context.Context, requestID string, apiKeyID *int64) (*service.UsageRequestCapture, error) {
	requestID = strings.TrimSpace(requestID)
	query, args := buildUsageRequestCaptureKeyQuery("SELECT "+usageRequestCaptureSelectColumns+" FROM usage_request_captures WHERE request_id = $1", requestID, apiKeyID)
	query += " ORDER BY id DESC LIMIT 1"

	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrUsageRequestCaptureNotFound
	}
	capture, err := scanUsageRequestCapture(rows)
	if err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return capture, nil
}

func (r *usageRequestCaptureRepository) ExistsByRequestID(ctx context.Context, requestID string, apiKeyID *int64) (bool, error) {
	requestID = strings.TrimSpace(requestID)
	query, args := buildUsageRequestCaptureKeyQuery("SELECT 1 FROM usage_request_captures WHERE request_id = $1", requestID, apiKeyID)
	query += " LIMIT 1"

	var one int
	if err := scanSingleRow(ctx, r.sql, query, args, &one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *usageRequestCaptureRepository) DeleteExpired(ctx context.Context, before time.Time, batchSize int) (int, error) {
	if batchSize <= 0 {
		return 0, nil
	}
	query := `
		WITH target AS (
			SELECT id
			FROM usage_request_captures
			WHERE expires_at IS NOT NULL
			  AND expires_at < $1
			ORDER BY expires_at ASC, id ASC
			LIMIT $2
		)
		DELETE FROM usage_request_captures
		WHERE id IN (SELECT id FROM target)
		RETURNING id
	`
	rows, err := r.sql.QueryContext(ctx, query, before, batchSize)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	deleted := 0
	for rows.Next() {
		deleted++
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func buildUsageRequestCaptureKeyQuery(base string, requestID string, apiKeyID *int64) (string, []any) {
	args := []any{requestID}
	if apiKeyID == nil {
		return base + " AND api_key_id IS NULL", args
	}
	args = append(args, *apiKeyID)
	return base + " AND api_key_id = $2", args
}

func prepareUsageRequestCaptureInsert(capture *service.UsageRequestCapture) usageRequestCaptureInsertPrepared {
	createdAt := capture.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	requestID := strings.TrimSpace(capture.RequestID)
	capture.RequestID = requestID
	version := capture.CaptureSchemaVersion
	if version == 0 {
		version = 1
	}
	capture.CaptureSchemaVersion = version
	apiKeyID := nullInt64(capture.APIKeyID)

	return usageRequestCaptureInsertPrepared{
		requestID: requestID,
		apiKeyID:  apiKeyID,
		createdAt: createdAt,
		args: []any{
			requestID,
			apiKeyID,
			nullInt64(capture.UsageLogID),
			nullInt64(capture.UserID),
			nullInt64(capture.AccountID),
			strings.TrimSpace(capture.Provider),
			strings.TrimSpace(capture.Model),
			strings.TrimSpace(capture.Endpoint),
			capture.Stream,
			capture.StatusCode,
			capture.DurationMs,
			capture.RequestBytes,
			capture.ResponseBytes,
			capture.CompressedBytes,
			capture.Truncated,
			nullString(capture.TruncateReason),
			version,
			capture.PayloadGzip,
			createdAt,
			nullTime(capture.ExpiresAt),
		},
	}
}

func scanUsageRequestCapture(scanner interface{ Scan(...any) error }) (*service.UsageRequestCapture, error) {
	var (
		capture        service.UsageRequestCapture
		apiKeyID       sql.NullInt64
		usageLogID     sql.NullInt64
		userID         sql.NullInt64
		accountID      sql.NullInt64
		truncateReason sql.NullString
		expiresAt      sql.NullTime
	)
	if err := scanner.Scan(
		&capture.ID,
		&capture.RequestID,
		&apiKeyID,
		&usageLogID,
		&userID,
		&accountID,
		&capture.Provider,
		&capture.Model,
		&capture.Endpoint,
		&capture.Stream,
		&capture.StatusCode,
		&capture.DurationMs,
		&capture.RequestBytes,
		&capture.ResponseBytes,
		&capture.CompressedBytes,
		&capture.Truncated,
		&truncateReason,
		&capture.CaptureSchemaVersion,
		&capture.PayloadGzip,
		&capture.CreatedAt,
		&expiresAt,
	); err != nil {
		return nil, err
	}
	capture.APIKeyID = int64PtrFromNull(apiKeyID)
	capture.UsageLogID = int64PtrFromNull(usageLogID)
	capture.UserID = int64PtrFromNull(userID)
	capture.AccountID = int64PtrFromNull(accountID)
	capture.TruncateReason = stringPtrFromNull(truncateReason)
	capture.ExpiresAt = timePtrFromNull(expiresAt)
	return &capture, nil
}

func nullTime(v *time.Time) sql.NullTime {
	if v == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *v, Valid: true}
}

func int64PtrFromNull(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func stringPtrFromNull(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	out := v.String
	return &out
}

func timePtrFromNull(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	out := v.Time
	return &out
}
