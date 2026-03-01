//go:build unit

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractOrigin(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
		{"valid https", "https://pay.example.com/checkout", "https://pay.example.com"},
		{"valid http", "http://pay.example.com/checkout", "http://pay.example.com"},
		{"https with port", "https://pay.example.com:8443/checkout", "https://pay.example.com:8443"},
		{"protocol-relative //host", "//pay.example.com/path", ""},
		{"no scheme", "pay.example.com/path", ""},
		{"ftp scheme rejected", "ftp://pay.example.com/file", ""},
		{"empty host after parse", "https:///path", ""},
		{"invalid url", "://bad url", ""},
		{"only scheme", "https://", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractOrigin(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
