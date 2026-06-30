package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const usageRequestCaptureShareSelectColumns = "id, share_id, request_id, api_key_id, created_by, label, expires_at, revoked_at, view_count, last_viewed_at, created_at"

type usageRequestCaptureShareRepository struct {
	client *dbent.Client
	sql    sqlExecutor
}

func NewUsageRequestCaptureShareRepository(client *dbent.Client, sqlDB *sql.DB) service.UsageRequestCaptureShareRepository {
	return newUsageRequestCaptureShareRepositoryWithSQL(client, sqlDB)
}

func newUsageRequestCaptureShareRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor) *usageRequestCaptureShareRepository {
	return &usageRequestCaptureShareRepository{client: client, sql: sqlq}
}

func (r *usageRequestCaptureShareRepository) Create(ctx context.Context, share *service.UsageRequestCaptureShare) error {
	if share == nil {
		return nil
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.create(ctx, tx.Client(), share)
	}
	return r.create(ctx, r.sql, share)
}

func (r *usageRequestCaptureShareRepository) GetByShareID(ctx context.Context, shareID string) (*service.UsageRequestCaptureShare, error) {
	shareID = strings.TrimSpace(shareID)
	query := "SELECT " + usageRequestCaptureShareSelectColumns + " FROM usage_request_capture_shares WHERE share_id = $1 LIMIT 1"
	rows, err := r.sql.QueryContext(ctx, query, shareID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrUsageRequestCaptureShareNotFound
	}
	share, err := scanUsageRequestCaptureShare(rows)
	if err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return share, nil
}

func (r *usageRequestCaptureShareRepository) List(ctx context.Context, filter service.ShareListFilter, page, pageSize int) ([]*service.UsageRequestCaptureShare, int64, error) {
	page, pageSize = normalizeShareListPage(page, pageSize)
	where, args := buildUsageRequestCaptureShareListWhere(filter)

	var total int64
	if err := scanSingleRow(ctx, r.sql, "SELECT COUNT(*) FROM usage_request_capture_shares"+where, args, &total); err != nil {
		return nil, 0, err
	}

	query := "SELECT " + usageRequestCaptureShareSelectColumns + " FROM usage_request_capture_shares" + where + " ORDER BY created_at DESC LIMIT $" + fmt.Sprint(len(args)+1) + " OFFSET $" + fmt.Sprint(len(args)+2)
	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := r.sql.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	shares := make([]*service.UsageRequestCaptureShare, 0)
	for rows.Next() {
		share, err := scanUsageRequestCaptureShare(rows)
		if err != nil {
			return nil, 0, err
		}
		shares = append(shares, share)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return shares, total, nil
}

func (r *usageRequestCaptureShareRepository) Revoke(ctx context.Context, id int64, revokedAt time.Time) error {
	query := `
		UPDATE usage_request_capture_shares
		SET revoked_at = $2
		WHERE id = $1 AND revoked_at IS NULL
	`
	return r.exec(ctx, query, id, revokedAt)
}

func (r *usageRequestCaptureShareRepository) IncrementView(ctx context.Context, shareID string, viewedAt time.Time) error {
	query := `
		UPDATE usage_request_capture_shares
		SET view_count = view_count + 1,
		    last_viewed_at = $2
		WHERE share_id = $1
	`
	return r.exec(ctx, query, strings.TrimSpace(shareID), viewedAt)
}

func (r *usageRequestCaptureShareRepository) create(ctx context.Context, sqlq sqlExecutor, share *service.UsageRequestCaptureShare) error {
	if sqlq == nil {
		sqlq = r.sql
	}
	createdAt := share.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	share.ShareID = strings.TrimSpace(share.ShareID)
	share.RequestID = strings.TrimSpace(share.RequestID)
	query := `
		INSERT INTO usage_request_capture_shares (
			share_id,
			request_id,
			api_key_id,
			created_by,
			label,
			expires_at,
			revoked_at,
			view_count,
			last_viewed_at,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
		RETURNING id, created_at
	`
	args := []any{
		share.ShareID,
		share.RequestID,
		nullInt64(share.APIKeyID),
		nullInt64(share.CreatedBy),
		nullString(share.Label),
		nullTime(share.ExpiresAt),
		nullTime(share.RevokedAt),
		share.ViewCount,
		nullTime(share.LastViewedAt),
		createdAt,
	}
	if err := scanSingleRow(ctx, sqlq, query, args, &share.ID, &share.CreatedAt); err != nil {
		return err
	}
	return nil
}

func (r *usageRequestCaptureShareRepository) exec(ctx context.Context, query string, args ...any) error {
	sqlq := r.sql
	if tx := dbent.TxFromContext(ctx); tx != nil {
		sqlq = tx.Client()
	}
	_, err := sqlq.ExecContext(ctx, query, args...)
	return err
}

func buildUsageRequestCaptureShareListWhere(filter service.ShareListFilter) (string, []any) {
	requestID := strings.TrimSpace(filter.RequestID)
	if requestID == "" {
		return "", nil
	}
	return " WHERE request_id = $1", []any{requestID}
}

func normalizeShareListPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	return page, pageSize
}

func scanUsageRequestCaptureShare(scanner interface{ Scan(...any) error }) (*service.UsageRequestCaptureShare, error) {
	var (
		share        service.UsageRequestCaptureShare
		apiKeyID     sql.NullInt64
		createdBy    sql.NullInt64
		label        sql.NullString
		expiresAt    sql.NullTime
		revokedAt    sql.NullTime
		lastViewedAt sql.NullTime
	)
	if err := scanner.Scan(
		&share.ID,
		&share.ShareID,
		&share.RequestID,
		&apiKeyID,
		&createdBy,
		&label,
		&expiresAt,
		&revokedAt,
		&share.ViewCount,
		&lastViewedAt,
		&share.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrUsageRequestCaptureShareNotFound
		}
		return nil, err
	}
	share.APIKeyID = int64PtrFromNull(apiKeyID)
	share.CreatedBy = int64PtrFromNull(createdBy)
	share.Label = stringPtrFromNull(label)
	share.ExpiresAt = timePtrFromNull(expiresAt)
	share.RevokedAt = timePtrFromNull(revokedAt)
	share.LastViewedAt = timePtrFromNull(lastViewedAt)
	return &share, nil
}
