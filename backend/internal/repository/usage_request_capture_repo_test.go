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

func TestUsageRequestCaptureRepositoryCreateBestEffort(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureRepositoryWithSQL(nil, db)

	apiKeyID := int64(2)
	userID := int64(3)
	accountID := int64(4)
	usageLogID := int64(5)
	reason := "response_too_large"
	expiresAt := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	capture := &service.UsageRequestCapture{
		RequestID:            " req-capture ",
		APIKeyID:             &apiKeyID,
		UsageLogID:           &usageLogID,
		UserID:               &userID,
		AccountID:            &accountID,
		Provider:             " anthropic ",
		Model:                " claude-sonnet-4-5 ",
		Endpoint:             " /v1/messages ",
		Stream:               true,
		StatusCode:           200,
		DurationMs:           1234,
		RequestBytes:         10,
		ResponseBytes:        20,
		CompressedBytes:      12,
		Truncated:            true,
		TruncateReason:       &reason,
		CaptureSchemaVersion: 2,
		PayloadGzip:          []byte{0x1f, 0x8b},
		ExpiresAt:            &expiresAt,
	}

	mock.ExpectQuery("INSERT INTO usage_request_captures").
		WithArgs(
			"req-capture",
			sql.NullInt64{Int64: apiKeyID, Valid: true},
			sql.NullInt64{Int64: usageLogID, Valid: true},
			sql.NullInt64{Int64: userID, Valid: true},
			sql.NullInt64{Int64: accountID, Valid: true},
			"anthropic",
			"claude-sonnet-4-5",
			"/v1/messages",
			true,
			200,
			int64(1234),
			int64(10),
			int64(20),
			int64(12),
			true,
			sql.NullString{String: reason, Valid: true},
			2,
			[]byte{0x1f, 0x8b},
			sqlmock.AnyArg(),
			sql.NullTime{Time: expiresAt, Valid: true},
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(9), createdAt))

	err := repo.CreateBestEffort(context.Background(), capture)
	require.NoError(t, err)
	require.Equal(t, int64(9), capture.ID)
	require.Equal(t, "req-capture", capture.RequestID)
	require.Equal(t, 2, capture.CaptureSchemaVersion)
	require.Equal(t, createdAt, capture.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureRepositoryCreateBestEffortDuplicate(t *testing.T) {
	tests := []struct {
		name     string
		apiKeyID *int64
		expect   func(sqlmock.Sqlmock, time.Time, *int64)
	}{
		{
			name:     "api key id",
			apiKeyID: int64Ptr(7),
			expect: func(mock sqlmock.Sqlmock, createdAt time.Time, apiKeyID *int64) {
				mock.ExpectQuery("SELECT id, created_at FROM usage_request_captures").
					WithArgs("req-duplicate", *apiKeyID).
					WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(11), createdAt))
			},
		},
		{
			name: "nil api key id",
			expect: func(mock sqlmock.Sqlmock, createdAt time.Time, _ *int64) {
				mock.ExpectQuery("api_key_id IS NULL").
					WithArgs("req-duplicate").
					WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(12), createdAt))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := newUsageRequestCaptureRepositoryWithSQL(nil, db)
			createdAt := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
			capture := &service.UsageRequestCapture{
				RequestID:       "req-duplicate",
				APIKeyID:        tc.apiKeyID,
				Provider:        "openai",
				Model:           "gpt-5",
				Endpoint:        "/v1/responses",
				StatusCode:      200,
				PayloadGzip:     []byte{1, 2, 3},
				CompressedBytes: 3,
			}

			mock.ExpectQuery("INSERT INTO usage_request_captures").
				WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}))
			tc.expect(mock, createdAt, tc.apiKeyID)

			err := repo.CreateBestEffort(context.Background(), capture)
			require.NoError(t, err)
			require.NotZero(t, capture.ID)
			require.Equal(t, createdAt, capture.CreatedAt)
			require.Equal(t, 1, capture.CaptureSchemaVersion)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageRequestCaptureRepositoryCreateBestEffortUsesDoNothing(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureRepositoryWithSQL(nil, db)
	capture := &service.UsageRequestCapture{
		RequestID:       "req-conflict-style",
		Provider:        "openai",
		Model:           "gpt-5",
		Endpoint:        "/v1/responses",
		StatusCode:      200,
		PayloadGzip:     []byte{1},
		CompressedBytes: 1,
	}

	mock.ExpectQuery(regexp.QuoteMeta("ON CONFLICT (request_id, api_key_id) DO NOTHING")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(1), time.Now().UTC()))

	err := repo.CreateBestEffort(context.Background(), capture)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureRepositoryGetByRequestID(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureRepositoryWithSQL(nil, db)

	apiKeyID := int64(2)
	usageLogID := int64(5)
	userID := int64(3)
	accountID := int64(4)
	reason := "request_too_large"
	createdAt := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{
		"id", "request_id", "api_key_id", "usage_log_id", "user_id", "account_id",
		"provider", "model", "endpoint", "stream", "status_code", "duration_ms",
		"request_bytes", "response_bytes", "compressed_bytes", "truncated", "truncate_reason",
		"capture_schema_version", "payload_gzip", "created_at", "expires_at",
	}).AddRow(
		int64(9), "req-get", apiKeyID, usageLogID, userID, accountID,
		"anthropic", "claude-sonnet-4-5", "/v1/messages", true, 200, int64(1234),
		int64(10), int64(20), int64(12), true, reason,
		2, []byte{0x1f, 0x8b}, createdAt, expiresAt,
	)

	mock.ExpectQuery("SELECT id, request_id, api_key_id").
		WithArgs("req-get", apiKeyID).
		WillReturnRows(rows)

	capture, err := repo.GetByRequestID(context.Background(), " req-get ", &apiKeyID)
	require.NoError(t, err)
	require.Equal(t, int64(9), capture.ID)
	require.Equal(t, "req-get", capture.RequestID)
	require.NotNil(t, capture.APIKeyID)
	require.Equal(t, apiKeyID, *capture.APIKeyID)
	require.NotNil(t, capture.UsageLogID)
	require.Equal(t, usageLogID, *capture.UsageLogID)
	require.NotNil(t, capture.UserID)
	require.Equal(t, userID, *capture.UserID)
	require.NotNil(t, capture.AccountID)
	require.Equal(t, accountID, *capture.AccountID)
	require.Equal(t, []byte{0x1f, 0x8b}, capture.PayloadGzip)
	require.NotNil(t, capture.TruncateReason)
	require.Equal(t, reason, *capture.TruncateReason)
	require.NotNil(t, capture.ExpiresAt)
	require.Equal(t, expiresAt, *capture.ExpiresAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureRepositoryGetByRequestIDNilAPIKey(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureRepositoryWithSQL(nil, db)
	createdAt := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{
		"id", "request_id", "api_key_id", "usage_log_id", "user_id", "account_id",
		"provider", "model", "endpoint", "stream", "status_code", "duration_ms",
		"request_bytes", "response_bytes", "compressed_bytes", "truncated", "truncate_reason",
		"capture_schema_version", "payload_gzip", "created_at", "expires_at",
	}).AddRow(
		int64(10), "req-nil", nil, nil, nil, nil,
		"openai", "gpt-5", "/v1/responses", false, 201, int64(5),
		int64(1), int64(2), int64(3), false, nil,
		1, []byte{1, 2, 3}, createdAt, nil,
	)

	mock.ExpectQuery("api_key_id IS NULL").
		WithArgs("req-nil").
		WillReturnRows(rows)

	capture, err := repo.GetByRequestID(context.Background(), "req-nil", nil)
	require.NoError(t, err)
	require.Nil(t, capture.APIKeyID)
	require.Nil(t, capture.ExpiresAt)
	require.Equal(t, []byte{1, 2, 3}, capture.PayloadGzip)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureRepositoryExistsByRequestIDDoesNotLoadPayload(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureRepositoryWithSQL(nil, db)

	apiKeyID := int64(2)
	mock.ExpectQuery("SELECT 1 FROM usage_request_captures").
		WithArgs("req-exists", apiKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"one"}).AddRow(1))

	exists, err := repo.ExistsByRequestID(context.Background(), "req-exists", &apiKeyID)
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureRepositoryExistsByRequestIDFalse(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureRepositoryWithSQL(nil, db)

	mock.ExpectQuery("api_key_id IS NULL").
		WithArgs("req-missing").
		WillReturnRows(sqlmock.NewRows([]string{"one"}))

	exists, err := repo.ExistsByRequestID(context.Background(), "req-missing", nil)
	require.NoError(t, err)
	require.False(t, exists)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureRepositoryDeleteExpired(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureRepositoryWithSQL(nil, db)

	before := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery("WITH target AS").
		WithArgs(before, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2)))

	deleted, err := repo.DeleteExpired(context.Background(), before, 2)
	require.NoError(t, err)
	require.Equal(t, 2, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageRequestCaptureRepositoryDeleteExpiredInvalidBatch(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageRequestCaptureRepositoryWithSQL(nil, db)

	deleted, err := repo.DeleteExpired(context.Background(), time.Now(), 0)
	require.NoError(t, err)
	require.Zero(t, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func int64Ptr(v int64) *int64 {
	return &v
}
