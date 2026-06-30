package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageRequestCaptureShareRepositoryCreate(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureShareRepositoryWithSQL(nil, db)

	apiKeyID := int64(2)
	createdBy := int64(3)
	label := "incident review"
	expiresAt := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	revokedAt := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)
	lastViewedAt := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	share := &service.UsageRequestCaptureShare{
		ShareID:      " share-abc ",
		RequestID:    " req-capture ",
		APIKeyID:     &apiKeyID,
		CreatedBy:    &createdBy,
		Label:        &label,
		ExpiresAt:    &expiresAt,
		RevokedAt:    &revokedAt,
		ViewCount:    4,
		LastViewedAt: &lastViewedAt,
	}

	mock.ExpectQuery("INSERT INTO usage_request_capture_shares").
		WithArgs(
			"share-abc",
			"req-capture",
			sql.NullInt64{Int64: apiKeyID, Valid: true},
			sql.NullInt64{Int64: createdBy, Valid: true},
			sql.NullString{String: label, Valid: true},
			sql.NullTime{Time: expiresAt, Valid: true},
			sql.NullTime{Time: revokedAt, Valid: true},
			4,
			sql.NullTime{Time: lastViewedAt, Valid: true},
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(9), createdAt))

	err := repo.Create(context.Background(), share)
	require.NoError(t, err)
	require.Equal(t, int64(9), share.ID)
	require.Equal(t, "share-abc", share.ShareID)
	require.Equal(t, "req-capture", share.RequestID)
	require.Equal(t, createdAt, share.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureShareRepositoryGetByShareID(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureShareRepositoryWithSQL(nil, db)

	apiKeyID := int64(2)
	createdBy := int64(3)
	label := "incident review"
	expiresAt := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	revokedAt := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)
	lastViewedAt := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	rows := shareRows().AddRow(
		int64(9), "share-get", "req-get", apiKeyID, createdBy, label,
		expiresAt, revokedAt, 4, lastViewedAt, createdAt,
	)

	mock.ExpectQuery("SELECT id, share_id, request_id").
		WithArgs("share-get").
		WillReturnRows(rows)

	share, err := repo.GetByShareID(context.Background(), " share-get ")
	require.NoError(t, err)
	require.Equal(t, int64(9), share.ID)
	require.Equal(t, "share-get", share.ShareID)
	require.NotNil(t, share.APIKeyID)
	require.Equal(t, apiKeyID, *share.APIKeyID)
	require.NotNil(t, share.CreatedBy)
	require.Equal(t, createdBy, *share.CreatedBy)
	require.NotNil(t, share.Label)
	require.Equal(t, label, *share.Label)
	require.NotNil(t, share.ExpiresAt)
	require.Equal(t, expiresAt, *share.ExpiresAt)
	require.NotNil(t, share.RevokedAt)
	require.Equal(t, revokedAt, *share.RevokedAt)
	require.Equal(t, 4, share.ViewCount)
	require.NotNil(t, share.LastViewedAt)
	require.Equal(t, lastViewedAt, *share.LastViewedAt)
	require.Equal(t, createdAt, share.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureShareRepositoryGetByShareIDNotFound(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureShareRepositoryWithSQL(nil, db)

	mock.ExpectQuery("SELECT id, share_id, request_id").
		WithArgs("missing-share").
		WillReturnRows(shareRows())

	share, err := repo.GetByShareID(context.Background(), "missing-share")
	require.Nil(t, share)
	require.ErrorIs(t, err, service.ErrUsageRequestCaptureShareNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureShareRepositoryList(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureShareRepositoryWithSQL(nil, db)
	createdAt := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM usage_request_capture_shares WHERE request_id = $1")).
		WithArgs("req-list").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))
	mock.ExpectQuery("ORDER BY created_at DESC").
		WithArgs("req-list", 10, 10).
		WillReturnRows(shareRows().AddRow(
			int64(9), "share-b", "req-list", nil, nil, nil,
			nil, nil, 0, nil, createdAt,
		))

	shares, total, err := repo.List(context.Background(), service.ShareListFilter{RequestID: " req-list "}, 2, 10)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, shares, 1)
	require.Equal(t, "share-b", shares[0].ShareID)
	require.Nil(t, shares[0].APIKeyID)
	require.Nil(t, shares[0].Label)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureShareRepositoryRevoke(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureShareRepositoryWithSQL(nil, db)
	revokedAt := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE usage_request_capture_shares").
		WithArgs(int64(9), revokedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Revoke(context.Background(), 9, revokedAt)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureShareRepositoryIncrementView(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureShareRepositoryWithSQL(nil, db)
	viewedAt := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)

	mock.ExpectExec("UPDATE usage_request_capture_shares").
		WithArgs("share-view", viewedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.IncrementView(context.Background(), " share-view ", viewedAt)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func shareRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "share_id", "request_id", "api_key_id", "created_by", "label",
		"expires_at", "revoked_at", "view_count", "last_viewed_at", "created_at",
	})
}
