package service

import "time"

type UsageRequestCaptureShare struct {
	ID           int64
	ShareID      string
	RequestID    string
	APIKeyID     *int64
	CreatedBy    *int64
	Label        *string
	ExpiresAt    *time.Time
	RevokedAt    *time.Time
	ViewCount    int
	LastViewedAt *time.Time
	CreatedAt    time.Time
}

type ShareListFilter struct {
	RequestID string
}
