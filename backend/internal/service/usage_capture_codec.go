package service

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/web/claudetap"
)

func usageCaptureRawJSONOrString(body []byte) (json.RawMessage, error) {
	if json.Valid(body) {
		return append(json.RawMessage(nil), body...), nil
	}
	encoded, err := json.Marshal(string(body))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func usageCaptureMarshalWithByteCap(record *claudetap.TraceRecord, maxRecordBytes int, requestBytes int64, responseBytes int64) ([]byte, bool, *string, error) {
	payloadJSON, err := json.Marshal(record)
	if err != nil {
		return nil, false, nil, err
	}
	if maxRecordBytes <= 0 || len(payloadJSON) <= maxRecordBytes {
		return payloadJSON, false, nil, nil
	}

	reason := usageCaptureTruncateReason
	record.Response.Body = usageCaptureTruncatedRaw(responseBytes)
	payloadJSON, err = json.Marshal(record)
	if err != nil {
		return nil, true, &reason, err
	}
	if len(payloadJSON) > maxRecordBytes {
		record.Request.Body = usageCaptureTruncatedRaw(requestBytes)
		payloadJSON, err = json.Marshal(record)
		if err != nil {
			return nil, true, &reason, err
		}
	}
	return payloadJSON, true, &reason, nil
}

func usageCaptureTruncatedRaw(originalBytes int64) json.RawMessage {
	encoded, err := json.Marshal(usageCaptureTruncatedPayload{
		Truncated:     true,
		OriginalBytes: originalBytes,
		Note:          usageCaptureTruncatedPayloadNote,
	})
	if err != nil {
		return json.RawMessage(`{"_truncated":true}`)
	}
	return json.RawMessage(encoded)
}

func usageCaptureGzip(payload []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func usageCaptureGunzip(payload []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	decoded, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return decoded, nil
}

func usageCaptureExpiresAt(now time.Time, retentionDays int) *time.Time {
	if retentionDays <= 0 {
		return nil
	}
	expiresAt := now.Add(time.Duration(retentionDays) * 24 * time.Hour)
	return &expiresAt
}

func usageCaptureRedactHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	redacted := make(map[string]string, len(headers))
	for name, value := range headers {
		if usageCaptureShouldRedactHeader(name) {
			redacted[name] = usageCaptureRedactedHeaderValue
			continue
		}
		redacted[name] = value
	}
	return redacted
}

func usageCaptureShouldRedactHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "x-api-key", "x-pulse-token", "cookie", "set-cookie", "proxy-authorization", "x-goog-api-key":
		return true
	default:
		return false
	}
}
