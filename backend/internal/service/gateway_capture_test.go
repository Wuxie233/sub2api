package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCaptureAccumulatorAppendStreamWithinCap(t *testing.T) {
	acc := NewCaptureAccumulator([]byte("req"), nil, "POST", "/v1/messages", 1024)
	acc.AppendStream([]byte("event: a\n"))
	acc.AppendStream([]byte("data: b\n"))

	snap := acc.Snapshot()
	require.Equal(t, "event: a\ndata: b\n", string(snap.ResponseBody))
	require.False(t, snap.Truncated)
}

func TestCaptureAccumulatorAppendStreamTruncatesAtCap(t *testing.T) {
	acc := NewCaptureAccumulator([]byte("req"), nil, "POST", "/v1/messages", 4)
	acc.AppendStream([]byte("abc"))  // fits (3/4)
	acc.AppendStream([]byte("XYZ"))  // only "X" fits, rest dropped
	acc.AppendStream([]byte("more")) // already at cap, dropped entirely

	snap := acc.Snapshot()
	require.Equal(t, "abcX", string(snap.ResponseBody))
	require.Len(t, snap.ResponseBody, 4)
	require.True(t, snap.Truncated)
}

// AppendStream must never mutate the caller's slice: the same bytes are handed
// to the client write, so any mutation would corrupt forwarded output.
func TestCaptureAccumulatorAppendStreamDoesNotMutateInput(t *testing.T) {
	acc := NewCaptureAccumulator(nil, nil, "POST", "/v1/messages", 2) // tiny cap forces the truncation branch
	chunk := []byte("hello")
	original := append([]byte(nil), chunk...)

	acc.AppendStream(chunk)
	require.Equal(t, original, chunk, "AppendStream must not modify bytes already written to the client")
}

// SetNonStreamResponse must copy (not alias) and not mutate the forwarded body.
func TestCaptureAccumulatorSetNonStreamResponseCopiesAndCaps(t *testing.T) {
	acc := NewCaptureAccumulator(nil, nil, "POST", "/v1/messages", 3)
	body := []byte("response-body")
	original := append([]byte(nil), body...)

	acc.SetNonStreamResponse(body, 200, map[string]string{"Content-Type": "application/json"})
	require.Equal(t, original, body, "SetNonStreamResponse must not mutate the forwarded body")

	snap := acc.Snapshot()
	require.Equal(t, "res", string(snap.ResponseBody))
	require.Equal(t, 200, snap.StatusCode)
	require.Equal(t, "application/json", snap.ResponseHeaders["Content-Type"])
	require.True(t, snap.Truncated)
}

// Snapshot returns an immutable copy: mutating the snapshot must not affect the
// accumulator, and later accumulator state must not leak into an old snapshot.
func TestCaptureAccumulatorSnapshotImmutability(t *testing.T) {
	acc := NewCaptureAccumulator([]byte("orig-req"), map[string]string{"H": "v"}, "POST", "/v1/messages", 1024)
	acc.SetNonStreamResponse([]byte("orig-resp"), 200, map[string]string{"R": "v"})

	snap := acc.Snapshot()
	snap.RequestBody[0] = 'X'
	snap.ResponseBody[0] = 'X'
	snap.RequestHeaders["H"] = "changed"
	snap.ResponseHeaders["R"] = "changed"

	snap2 := acc.Snapshot()
	require.Equal(t, "orig-req", string(snap2.RequestBody))
	require.Equal(t, "orig-resp", string(snap2.ResponseBody))
	require.Equal(t, "v", snap2.RequestHeaders["H"])
	require.Equal(t, "v", snap2.ResponseHeaders["R"])
}

func TestCaptureAccumulatorNewCapsRequestBodyAndMarksTruncated(t *testing.T) {
	acc := NewCaptureAccumulator([]byte("0123456789"), nil, "POST", "/v1/messages", 4)
	snap := acc.Snapshot()
	require.Equal(t, "0123", string(snap.RequestBody))
	require.True(t, snap.Truncated)
}

// When both a non-stream body and streamed bytes were recorded, the streamed
// (client-delivered) bytes win in the snapshot.
func TestCaptureAccumulatorSnapshotPrefersStreamBuffer(t *testing.T) {
	acc := NewCaptureAccumulator(nil, nil, "POST", "/v1/messages", 1024)
	acc.SetNonStreamResponse([]byte("nonstream"), 200, nil)
	acc.AppendStream([]byte("streamed"))

	snap := acc.Snapshot()
	require.Equal(t, "streamed", string(snap.ResponseBody))
}

func TestCaptureAccumulatorClientDisconnectFlag(t *testing.T) {
	acc := NewCaptureAccumulator(nil, nil, "POST", "/v1/messages", 1024)
	require.False(t, acc.Snapshot().ClientDisconnect)
	acc.SetClientDisconnect(true)
	require.True(t, acc.Snapshot().ClientDisconnect)
}

func TestCaptureAccumulatorCarrierHelpers(t *testing.T) {
	acc := NewCaptureAccumulator([]byte("req"), nil, "POST", "/v1/messages", 1024)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	SetGinCaptureAccumulator(c, acc)
	require.Same(t, acc, CaptureAccumulatorFromGin(c))

	ctx := WithCaptureAccumulator(context.Background(), acc)
	require.Same(t, acc, CaptureAccumulatorFromContext(ctx))
	require.Same(t, acc, CaptureAccumulatorFromGinOrContext(c, ctx))

	// nil-safety: helpers must tolerate missing carriers.
	require.Nil(t, CaptureAccumulatorFromGin(nil))
	require.Nil(t, CaptureAccumulatorFromContext(context.Background()))
	var nilAcc *CaptureAccumulator
	require.NotPanics(t, func() {
		nilAcc.AppendStream([]byte("x"))
		nilAcc.SetNonStreamResponse([]byte("x"), 200, nil)
		nilAcc.SetClientDisconnect(true)
		_ = nilAcc.Snapshot()
	})
}
