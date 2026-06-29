package service

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const gatewayCaptureAccumulatorKey = "service.gateway.capture_accumulator"

type gatewayCaptureContextKey struct{}

// CaptureAccumulator is a per-request carrier shared by the handler and native
// Anthropic forwarding path. It is mutated only by the request goroutine.
type CaptureAccumulator struct {
	RequestBody     []byte
	RequestHeaders  map[string]string
	Method          string
	Path            string
	ResponseBody    []byte
	StatusCode      int
	ResponseHeaders map[string]string

	streamBuf        bytes.Buffer
	maxBytes         int
	truncated        bool
	clientDisconnect bool
}

type CaptureSnapshot struct {
	RequestBody      []byte
	RequestHeaders   map[string]string
	Method           string
	Path             string
	ResponseBody     []byte
	StatusCode       int
	ResponseHeaders  map[string]string
	Truncated        bool
	ClientDisconnect bool
}

func NewCaptureAccumulator(requestBody []byte, requestHeaders map[string]string, method, path string, maxBytes int) *CaptureAccumulator {
	capBytes := normalizedCaptureCap(maxBytes)
	acc := &CaptureAccumulator{
		RequestBody:    copyBytesWithCap(requestBody, capBytes),
		RequestHeaders: cloneStringMap(requestHeaders),
		Method:         method,
		Path:           path,
		maxBytes:       capBytes,
	}
	if len(requestBody) > len(acc.RequestBody) {
		acc.truncated = true
	}
	return acc
}

func (a *CaptureAccumulator) AppendStream(chunk []byte) {
	if a == nil || len(chunk) == 0 || a.maxBytes <= 0 || a.streamBuf.Len() >= a.maxBytes {
		if a != nil && len(chunk) > 0 && a.streamBuf.Len() >= a.maxBytes {
			a.truncated = true
		}
		return
	}
	remaining := a.maxBytes - a.streamBuf.Len()
	if len(chunk) > remaining {
		_, _ = a.streamBuf.Write(chunk[:remaining])
		a.truncated = true
		return
	}
	_, _ = a.streamBuf.Write(chunk)
}

func (a *CaptureAccumulator) SetNonStreamResponse(body []byte, status int, headers map[string]string) {
	if a == nil {
		return
	}
	a.StatusCode = status
	a.ResponseHeaders = cloneStringMap(headers)
	a.ResponseBody = copyBytesWithCap(body, a.maxBytes)
	if len(body) > len(a.ResponseBody) {
		a.truncated = true
	}
}

func (a *CaptureAccumulator) SetStatusAndHeaders(status int, headers map[string]string) {
	if a == nil {
		return
	}
	a.StatusCode = status
	a.ResponseHeaders = cloneStringMap(headers)
}

func (a *CaptureAccumulator) SetClientDisconnect(disconnected bool) {
	if a == nil {
		return
	}
	a.clientDisconnect = disconnected
}

func (a *CaptureAccumulator) Snapshot() CaptureSnapshot {
	if a == nil {
		return CaptureSnapshot{}
	}
	responseBody := a.ResponseBody
	if a.streamBuf.Len() > 0 {
		responseBody = a.streamBuf.Bytes()
	}
	return CaptureSnapshot{
		RequestBody:      copyBytesWithCap(a.RequestBody, a.maxBytes),
		RequestHeaders:   cloneStringMap(a.RequestHeaders),
		Method:           a.Method,
		Path:             a.Path,
		ResponseBody:     copyBytesWithCap(responseBody, a.maxBytes),
		StatusCode:       a.StatusCode,
		ResponseHeaders:  cloneStringMap(a.ResponseHeaders),
		Truncated:        a.truncated,
		ClientDisconnect: a.clientDisconnect,
	}
}

func WithCaptureAccumulator(ctx context.Context, acc *CaptureAccumulator) context.Context {
	if ctx == nil || acc == nil {
		return ctx
	}
	return context.WithValue(ctx, gatewayCaptureContextKey{}, acc)
}

func SetGinCaptureAccumulator(c *gin.Context, acc *CaptureAccumulator) {
	if c == nil || acc == nil {
		return
	}
	c.Set(gatewayCaptureAccumulatorKey, acc)
}

func CaptureAccumulatorFromGin(c *gin.Context) *CaptureAccumulator {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(gatewayCaptureAccumulatorKey); ok {
		if acc, ok := value.(*CaptureAccumulator); ok {
			return acc
		}
	}
	return nil
}

func CaptureAccumulatorFromContext(ctx context.Context) *CaptureAccumulator {
	if ctx == nil {
		return nil
	}
	acc, _ := ctx.Value(gatewayCaptureContextKey{}).(*CaptureAccumulator)
	return acc
}

func CaptureAccumulatorFromGinOrContext(c *gin.Context, ctx context.Context) *CaptureAccumulator {
	if acc := CaptureAccumulatorFromGin(c); acc != nil {
		return acc
	}
	return CaptureAccumulatorFromContext(ctx)
}

func CloneHTTPHeaderForCapture(header http.Header) map[string]string {
	if len(header) == 0 {
		return nil
	}
	out := make(map[string]string, len(header))
	for key, values := range header {
		out[key] = strings.Join(values, ", ")
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyBytesWithCap(in []byte, maxBytes int) []byte {
	if len(in) == 0 || maxBytes <= 0 {
		return nil
	}
	if len(in) > maxBytes {
		in = in[:maxBytes]
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func normalizedCaptureCap(maxBytes int) int {
	if maxBytes > 0 {
		return maxBytes
	}
	return defaultUsageCaptureMaxRecordBytes
}
