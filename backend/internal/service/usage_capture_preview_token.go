package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const UsageCapturePreviewTokenTTL = 2 * time.Minute

type UsageCapturePreviewSigner struct {
	secret []byte
}

type UsageCapturePreviewClaims struct {
	RequestID string
	APIKeyID  *int64
	ExpiresAt time.Time
}

func NewUsageCapturePreviewSigner(secret string) *UsageCapturePreviewSigner {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return nil
	}
	return &UsageCapturePreviewSigner{secret: []byte(trimmed)}
}

func (s *UsageCapturePreviewSigner) Sign(claims UsageCapturePreviewClaims) string {
	payload := usageCapturePreviewTokenPayload(claims.RequestID, claims.APIKeyID, claims.ExpiresAt.Unix())
	sig := s.signature(payload)
	return payload + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (s *UsageCapturePreviewSigner) Verify(token string, requestID string, apiKeyID *int64, now time.Time) bool {
	if s == nil {
		return false
	}
	separator := strings.LastIndex(token, ".")
	if separator <= 0 || separator == len(token)-1 {
		return false
	}
	payload := token[:separator]
	sigEncoded := token[separator+1:]
	sig, err := base64.RawURLEncoding.DecodeString(sigEncoded)
	if err != nil {
		return false
	}
	wantSig := s.signature(payload)
	if !hmac.Equal(sig, wantSig) {
		return false
	}

	parsedRequestID, parsedAPIKeyID, expiresUnix, ok := parseUsageCapturePreviewTokenPayload(payload)
	if !ok {
		return false
	}
	if parsedRequestID != requestID || !usageCapturePreviewAPIKeyIDEqual(parsedAPIKeyID, apiKeyID) {
		return false
	}
	return now.Unix() <= expiresUnix
}

func (s *UsageCapturePreviewSigner) signature(payload string) []byte {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func usageCapturePreviewTokenPayload(requestID string, apiKeyID *int64, expiresUnix int64) string {
	apiKeyIDRaw := ""
	if apiKeyID != nil {
		apiKeyIDRaw = strconv.FormatInt(*apiKeyID, 10)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(requestID)) + "." + apiKeyIDRaw + "." + strconv.FormatInt(expiresUnix, 10)
}

func parseUsageCapturePreviewTokenPayload(payload string) (string, *int64, int64, bool) {
	parts := strings.Split(payload, ".")
	if len(parts) != 3 {
		return "", nil, 0, false
	}
	requestIDBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(requestIDBytes) == 0 {
		return "", nil, 0, false
	}
	expiresUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", nil, 0, false
	}
	var apiKeyID *int64
	if parts[1] != "" {
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return "", nil, 0, false
		}
		apiKeyID = &id
	}
	return string(requestIDBytes), apiKeyID, expiresUnix, true
}

func usageCapturePreviewAPIKeyIDEqual(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func ParseUsageCapturePreviewParams(rawRequestID, rawAPIKeyID string) (string, *int64, error) {
	requestID := strings.TrimSpace(rawRequestID)
	if requestID == "" {
		return "", nil, fmt.Errorf("request_id is required")
	}
	var apiKeyID *int64
	if raw := strings.TrimSpace(rawAPIKeyID); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return "", nil, fmt.Errorf("invalid api_key_id")
		}
		apiKeyID = &id
	}
	return requestID, apiKeyID, nil
}
