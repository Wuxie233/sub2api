//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsThinkingBlockSignatureError_DetectsFinalBlockThinking(t *testing.T) {
	body := []byte("{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"messages.133: The final block in an assistant message cannot be `thinking`.\"}}")

	require.True(t, (&GatewayService{}).isThinkingBlockSignatureError(body))
}
