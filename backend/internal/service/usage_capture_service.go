package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/web/claudetap"
)

const (
	usageCaptureSchemaVersion         = 1
	defaultUsageCaptureRetentionDays  = 30
	defaultUsageCaptureMaxRecordBytes = 5000000
	usageCaptureTruncateReason        = "exceeded usage_capture.max_record_bytes"
	usageCaptureTruncatedPayloadNote  = "omitted: exceeded usage_capture.max_record_bytes"
	usageCaptureRedactedHeaderValue   = "[REDACTED]"
)

type UsageCaptureService struct {
	repo UsageRequestCaptureRepository
	cfg  *config.Config
}

type CaptureInput struct {
	RequestID       string
	APIKeyID        *int64
	UsageLogID      *int64
	UserID          *int64
	AccountID       *int64
	Provider        string
	Model           string
	Endpoint        string
	Stream          bool
	StatusCode      int
	DurationMs      int64
	Method          string
	Path            string
	RequestHeaders  map[string]string
	ResponseHeaders map[string]string
	RequestBody     []byte
	ResponseBody    []byte
	Timestamp       time.Time
	Turn            int
	Truncated       bool
	ClientDisconnect bool
}

type usageCaptureTruncatedPayload struct {
	Truncated     bool   `json:"_truncated"`
	OriginalBytes int64  `json:"_original_bytes"`
	Note          string `json:"_note"`
}

func NewUsageCaptureService(repo UsageRequestCaptureRepository, cfg *config.Config) *UsageCaptureService {
	return &UsageCaptureService{repo: repo, cfg: cfg}
}

func (s *UsageCaptureService) Enabled() bool {
	return s != nil && (s.cfg == nil || s.cfg.UsageCapture.Enabled)
}

func (s *UsageCaptureService) Capture(ctx context.Context, in CaptureInput) error {
	if !s.Enabled() {
		return nil
	}
	if s.repo == nil {
		return fmt.Errorf("usage capture service not ready")
	}

	now := time.Now()
	timestamp := in.Timestamp
	if timestamp.IsZero() {
		timestamp = now
	}
	turn := in.Turn
	if turn == 0 {
		turn = 1
	}

	requestBody, err := usageCaptureRawJSONOrString(in.RequestBody)
	if err != nil {
		return fmt.Errorf("encode request body: %w", err)
	}
	responseBody, err := usageCaptureRawJSONOrString(in.ResponseBody)
	if err != nil {
		return fmt.Errorf("encode response body: %w", err)
	}

	record := claudetap.TraceRecord{
		Timestamp:  timestamp.UTC().Format(time.RFC3339),
		RequestID:  in.RequestID,
		Turn:       turn,
		DurationMs: in.DurationMs,
		Request: claudetap.TraceRequest{
			Method:  in.Method,
			Path:    in.Path,
			Headers: usageCaptureRedactHeaders(in.RequestHeaders),
			Body:    requestBody,
		},
		Response: claudetap.TraceResponse{
			Status:  in.StatusCode,
			Headers: usageCaptureRedactHeaders(in.ResponseHeaders),
			Body:    responseBody,
		},
	}
	if in.Truncated || in.ClientDisconnect {
		record.Capture = make(map[string]any, 2)
		if in.Truncated {
			record.Capture["truncated"] = true
		}
		if in.ClientDisconnect {
			record.Capture["client_disconnect"] = true
		}
	}

	payloadJSON, truncated, truncateReason, err := usageCaptureMarshalWithByteCap(&record, s.maxRecordBytes(), int64(len(in.RequestBody)), int64(len(in.ResponseBody)))
	if err != nil {
		return fmt.Errorf("marshal trace record: %w", err)
	}
	if in.Truncated && truncateReason == nil {
		reason := usageCaptureTruncateReason
		truncateReason = &reason
	}
	payloadGzip, err := usageCaptureGzip(payloadJSON)
	if err != nil {
		return fmt.Errorf("gzip trace record: %w", err)
	}

	capture := &UsageRequestCapture{
		RequestID:            in.RequestID,
		APIKeyID:             in.APIKeyID,
		UsageLogID:           in.UsageLogID,
		UserID:               in.UserID,
		AccountID:            in.AccountID,
		Provider:             in.Provider,
		Model:                in.Model,
		Endpoint:             in.Endpoint,
		Stream:               in.Stream,
		StatusCode:           in.StatusCode,
		DurationMs:           in.DurationMs,
		RequestBytes:         int64(len(in.RequestBody)),
		ResponseBytes:        int64(len(in.ResponseBody)),
		CompressedBytes:      int64(len(payloadGzip)),
		Truncated:            truncated || in.Truncated,
		TruncateReason:       truncateReason,
		CaptureSchemaVersion: usageCaptureSchemaVersion,
		PayloadGzip:          payloadGzip,
		CreatedAt:            now,
		ExpiresAt:            usageCaptureExpiresAt(now, s.retentionDays()),
	}
	return s.repo.CreateBestEffort(ctx, capture)
}

func (s *UsageCaptureService) CaptureBestEffort(ctx context.Context, in CaptureInput) {
	if err := s.Capture(ctx, in); err != nil {
		logger.LegacyPrintf("service.usage_capture", "[UsageCapture] capture failed: request_id=%s err=%v", in.RequestID, err)
	}
}

func (s *UsageCaptureService) Get(ctx context.Context, requestID string, apiKeyID *int64) (*claudetap.TraceRecord, *UsageRequestCapture, error) {
	if s == nil || s.repo == nil {
		return nil, nil, fmt.Errorf("usage capture service not ready")
	}

	capture, err := s.repo.GetByRequestID(ctx, requestID, apiKeyID)
	if err != nil {
		return nil, nil, err
	}
	payloadJSON, err := usageCaptureGunzip(capture.PayloadGzip)
	if err != nil {
		return nil, nil, fmt.Errorf("gunzip trace record: %w", err)
	}
	var record claudetap.TraceRecord
	if err := json.Unmarshal(payloadJSON, &record); err != nil {
		return nil, nil, fmt.Errorf("unmarshal trace record: %w", err)
	}
	return &record, capture, nil
}

// RenderHTML loads a captured request by request_id (and optional api_key_id) and
// renders it into the self-contained claudetap HTML viewer. It propagates
// ErrUsageRequestCaptureNotFound unchanged so callers can map it to a 404.
func (s *UsageCaptureService) RenderHTML(ctx context.Context, requestID string, apiKeyID *int64) (html []byte, capture *UsageRequestCapture, err error) {
	record, capture, err := s.Get(ctx, requestID, apiKeyID)
	if err != nil {
		return nil, nil, err
	}
	html, err = claudetap.RenderViewer([]claudetap.TraceRecord{*record})
	if err != nil {
		return nil, capture, fmt.Errorf("render usage capture viewer: %w", err)
	}
	return html, capture, nil
}

func (s *UsageCaptureService) Available(ctx context.Context, requestID string, apiKeyID *int64) (bool, error) {
	if s == nil || s.repo == nil {
		return false, fmt.Errorf("usage capture service not ready")
	}
	return s.repo.ExistsByRequestID(ctx, requestID, apiKeyID)
}

func (s *UsageCaptureService) MaxRecordBytes() int {
	return s.maxRecordBytes()
}

func (s *UsageCaptureService) maxRecordBytes() int {
	if s == nil || s.cfg == nil {
		return defaultUsageCaptureMaxRecordBytes
	}
	if s.cfg.UsageCapture.MaxRecordBytes > 0 {
		return s.cfg.UsageCapture.MaxRecordBytes
	}
	return defaultUsageCaptureMaxRecordBytes
}

func (s *UsageCaptureService) retentionDays() int {
	if s == nil || s.cfg == nil {
		return defaultUsageCaptureRetentionDays
	}
	return s.cfg.UsageCapture.RetentionDays
}
