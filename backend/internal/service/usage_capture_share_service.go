package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const usageCaptureShareIDBytes = 16

type UsageCaptureShareService struct {
	shareRepo      UsageRequestCaptureShareRepository
	captureService *UsageCaptureService
}

type CreateShareInput struct {
	RequestID string
	APIKeyID  *int64
	CreatedBy *int64
	Label     *string
	ExpiresAt *time.Time
}

func NewUsageCaptureShareService(shareRepo UsageRequestCaptureShareRepository, captureService *UsageCaptureService) *UsageCaptureShareService {
	return &UsageCaptureShareService{shareRepo: shareRepo, captureService: captureService}
}

func (s *UsageCaptureShareService) CreateShare(ctx context.Context, in CreateShareInput) (*UsageRequestCaptureShare, error) {
	if s == nil || s.shareRepo == nil || s.captureService == nil {
		return nil, fmt.Errorf("usage capture share service not ready")
	}
	requestID := strings.TrimSpace(in.RequestID)
	if requestID == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	available, err := s.captureService.Available(ctx, requestID, in.APIKeyID)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, ErrUsageRequestCaptureNotFound
	}
	shareID, err := generateUsageCaptureShareID()
	if err != nil {
		return nil, err
	}
	share := &UsageRequestCaptureShare{
		ShareID:   shareID,
		RequestID: requestID,
		APIKeyID:  in.APIKeyID,
		CreatedBy: in.CreatedBy,
		Label:     in.Label,
		ExpiresAt: in.ExpiresAt,
		CreatedAt: time.Now(),
	}
	if err := s.shareRepo.Create(ctx, share); err != nil {
		return nil, err
	}
	return share, nil
}

func (s *UsageCaptureShareService) ResolvePublic(ctx context.Context, shareID string) ([]byte, error) {
	if s == nil || s.shareRepo == nil || s.captureService == nil {
		return nil, fmt.Errorf("usage capture share service not ready")
	}
	share, err := s.shareRepo.GetByShareID(ctx, strings.TrimSpace(shareID))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if share.RevokedAt != nil || (share.ExpiresAt != nil && now.After(*share.ExpiresAt)) {
		return nil, ErrUsageRequestCaptureShareNotFound
	}
	html, _, err := s.captureService.RenderHTML(ctx, share.RequestID, share.APIKeyID)
	if err != nil {
		if errors.Is(err, ErrUsageRequestCaptureNotFound) {
			return nil, ErrUsageRequestCaptureShareNotFound
		}
		return nil, err
	}
	_ = s.shareRepo.IncrementView(ctx, share.ShareID, now)
	return html, nil
}

func (s *UsageCaptureShareService) ListShares(ctx context.Context, filter ShareListFilter, page, pageSize int) ([]*UsageRequestCaptureShare, int64, error) {
	if s == nil || s.shareRepo == nil {
		return nil, 0, fmt.Errorf("usage capture share service not ready")
	}
	return s.shareRepo.List(ctx, filter, page, pageSize)
}

func (s *UsageCaptureShareService) RevokeShare(ctx context.Context, id int64) error {
	if s == nil || s.shareRepo == nil {
		return fmt.Errorf("usage capture share service not ready")
	}
	return s.shareRepo.Revoke(ctx, id, time.Now())
}

func ShareStatus(s *UsageRequestCaptureShare, now time.Time) string {
	if s == nil {
		return "active"
	}
	if s.RevokedAt != nil {
		return "revoked"
	}
	if s.ExpiresAt != nil && now.After(*s.ExpiresAt) {
		return "expired"
	}
	return "active"
}

func generateUsageCaptureShareID() (string, error) {
	buf := make([]byte, usageCaptureShareIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate usage capture share id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
