package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type usageCaptureShareFakeRepo struct {
	mu             sync.Mutex
	shares         []*UsageRequestCaptureShare
	incrementErr   error
	incrementCalls int
}

func (r *usageCaptureShareFakeRepo) Create(ctx context.Context, share *UsageRequestCaptureShare) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if share.ID == 0 {
		share.ID = int64(len(r.shares) + 1)
	}
	if share.CreatedAt.IsZero() {
		share.CreatedAt = time.Now()
	}
	cloned := *share
	r.shares = append(r.shares, &cloned)
	return nil
}

func (r *usageCaptureShareFakeRepo) GetByShareID(ctx context.Context, shareID string) (*UsageRequestCaptureShare, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, share := range r.shares {
		if share.ShareID == shareID {
			cloned := *share
			return &cloned, nil
		}
	}
	return nil, ErrUsageRequestCaptureShareNotFound
}

func (r *usageCaptureShareFakeRepo) List(ctx context.Context, filter ShareListFilter, page, pageSize int) ([]*UsageRequestCaptureShare, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*UsageRequestCaptureShare, 0, len(r.shares))
	for _, share := range r.shares {
		if filter.RequestID != "" && share.RequestID != filter.RequestID {
			continue
		}
		cloned := *share
		items = append(items, &cloned)
	}
	return items, int64(len(items)), nil
}

func (r *usageCaptureShareFakeRepo) Revoke(ctx context.Context, id int64, revokedAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, share := range r.shares {
		if share.ID == id {
			share.RevokedAt = &revokedAt
			return nil
		}
	}
	return ErrUsageRequestCaptureShareNotFound
}

func (r *usageCaptureShareFakeRepo) IncrementView(ctx context.Context, shareID string, viewedAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.incrementCalls++
	if r.incrementErr != nil {
		return r.incrementErr
	}
	for _, share := range r.shares {
		if share.ShareID == shareID {
			share.ViewCount++
			share.LastViewedAt = &viewedAt
			return nil
		}
	}
	return ErrUsageRequestCaptureShareNotFound
}

func TestUsageCaptureShareServiceCreateSharePersistsGeneratedID(t *testing.T) {
	captureRepo := &usageCaptureFakeRepo{}
	captureSvc := NewUsageCaptureService(captureRepo, usageCaptureTestConfig(true, 7, 1_000_000))
	apiKeyID := int64(42)
	createdBy := int64(7)
	label := "audit"
	require.NoError(t, captureSvc.Capture(context.Background(), CaptureInput{RequestID: "req-share-create", APIKeyID: &apiKeyID, RequestBody: []byte(`{}`), ResponseBody: []byte(`{}`)}))
	shareRepo := &usageCaptureShareFakeRepo{}
	svc := NewUsageCaptureShareService(shareRepo, captureSvc)

	share, err := svc.CreateShare(context.Background(), CreateShareInput{RequestID: " req-share-create ", APIKeyID: &apiKeyID, CreatedBy: &createdBy, Label: &label})

	require.NoError(t, err)
	require.NotEmpty(t, share.ShareID)
	require.Len(t, share.ShareID, 22)
	require.Equal(t, "req-share-create", share.RequestID)
	require.Equal(t, createdBy, *share.CreatedBy)
	require.Equal(t, label, *share.Label)
	require.Len(t, shareRepo.shares, 1)
}

func TestUsageCaptureShareServiceCreateShareReturnsCaptureNotFound(t *testing.T) {
	svc := NewUsageCaptureShareService(&usageCaptureShareFakeRepo{}, NewUsageCaptureService(&usageCaptureFakeRepo{}, usageCaptureTestConfig(true, 7, 1_000_000)))

	_, err := svc.CreateShare(context.Background(), CreateShareInput{RequestID: "missing"})

	require.ErrorIs(t, err, ErrUsageRequestCaptureNotFound)
}

func TestUsageCaptureShareServiceResolvePublic(t *testing.T) {
	captureRepo := &usageCaptureFakeRepo{}
	captureSvc := NewUsageCaptureService(captureRepo, usageCaptureTestConfig(true, 7, 1_000_000))
	require.NoError(t, captureSvc.Capture(context.Background(), CaptureInput{RequestID: "req-share-resolve", RequestBody: []byte(`{"messages":[{"role":"user","content":"hi"}]}`), ResponseBody: []byte(`{"content":[{"type":"text","text":"hello"}]}`)}))
	shareRepo := &usageCaptureShareFakeRepo{incrementErr: errors.New("ignored")}
	svc := NewUsageCaptureShareService(shareRepo, captureSvc)
	share, err := svc.CreateShare(context.Background(), CreateShareInput{RequestID: "req-share-resolve"})
	require.NoError(t, err)

	html, err := svc.ResolvePublic(context.Background(), share.ShareID)

	require.NoError(t, err)
	require.Contains(t, string(html), "EMBEDDED_TRACE_DATA")
	require.Equal(t, 1, shareRepo.incrementCalls)
}

func TestUsageCaptureShareServiceResolvePublicReturnsUniformNotFound(t *testing.T) {
	captureRepo := &usageCaptureFakeRepo{}
	captureSvc := NewUsageCaptureService(captureRepo, usageCaptureTestConfig(true, 7, 1_000_000))
	require.NoError(t, captureSvc.Capture(context.Background(), CaptureInput{RequestID: "req-share-invalid", RequestBody: []byte(`{}`), ResponseBody: []byte(`{}`)}))
	shareRepo := &usageCaptureShareFakeRepo{}
	svc := NewUsageCaptureShareService(shareRepo, captureSvc)
	revoked, err := svc.CreateShare(context.Background(), CreateShareInput{RequestID: "req-share-invalid"})
	require.NoError(t, err)
	require.NoError(t, svc.RevokeShare(context.Background(), revoked.ID))
	expiredAt := time.Now().Add(-time.Minute)
	expired, err := svc.CreateShare(context.Background(), CreateShareInput{RequestID: "req-share-invalid", ExpiresAt: &expiredAt})
	require.NoError(t, err)
	missingCapture, err := svc.CreateShare(context.Background(), CreateShareInput{RequestID: "req-share-invalid"})
	require.NoError(t, err)
	captureRepo.captures = nil

	tests := []struct {
		name    string
		shareID string
	}{
		{name: "revoked", shareID: revoked.ShareID},
		{name: "expired", shareID: expired.ShareID},
		{name: "missing capture", shareID: missingCapture.ShareID},
		{name: "unknown", shareID: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.ResolvePublic(context.Background(), tt.shareID)

			require.ErrorIs(t, err, ErrUsageRequestCaptureShareNotFound)
		})
	}
}

func TestUsageCaptureShareServiceRevokeShare(t *testing.T) {
	captureRepo := &usageCaptureFakeRepo{}
	captureSvc := NewUsageCaptureService(captureRepo, usageCaptureTestConfig(true, 7, 1_000_000))
	require.NoError(t, captureSvc.Capture(context.Background(), CaptureInput{RequestID: "req-share-revoke", RequestBody: []byte(`{}`), ResponseBody: []byte(`{}`)}))
	shareRepo := &usageCaptureShareFakeRepo{}
	svc := NewUsageCaptureShareService(shareRepo, captureSvc)
	share, err := svc.CreateShare(context.Background(), CreateShareInput{RequestID: "req-share-revoke"})
	require.NoError(t, err)

	err = svc.RevokeShare(context.Background(), share.ID)

	require.NoError(t, err)
	got, err := shareRepo.GetByShareID(context.Background(), share.ShareID)
	require.NoError(t, err)
	require.NotNil(t, got.RevokedAt)
}

func TestShareStatus(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	tests := []struct {
		name  string
		share *UsageRequestCaptureShare
		want  string
	}{
		{name: "active", share: &UsageRequestCaptureShare{ExpiresAt: &future}, want: "active"},
		{name: "expired", share: &UsageRequestCaptureShare{ExpiresAt: &past}, want: "expired"},
		{name: "revoked", share: &UsageRequestCaptureShare{RevokedAt: &past, ExpiresAt: &future}, want: "revoked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShareStatus(tt.share, now)

			require.Equal(t, tt.want, got)
		})
	}
}
