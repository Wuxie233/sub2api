package handler

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/web/pulse"
	"github.com/gin-gonic/gin"
)

type PulseUsageProvider interface {
	GetUsage(ctx context.Context, identity *config.PulseAccessTokenConfig, refresh bool) (service.PulseUsageDTO, error)
}

type PulseHandler struct {
	cfg   *config.Config
	pulse PulseUsageProvider
}

func NewPulseHandler(cfg *config.Config, pulseService PulseUsageProvider) *PulseHandler {
	return &PulseHandler{cfg: cfg, pulse: pulseService}
}

func (h *PulseHandler) ServePage(c *gin.Context) {
	if h.disabled() {
		c.Status(http.StatusNotFound)
		return
	}
	nonce := middleware.GetNonceFromContext(c)
	html := bytes.ReplaceAll(pulse.DashboardHTML, []byte(pulse.NoncePlaceholder), []byte(nonce))
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}

func (h *PulseHandler) ServeUsage(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		if h.disabled() {
			c.Status(http.StatusNotFound)
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	refresh := c.Query("refresh") == "1" && identity.Role == config.PulseAccessRoleOwner
	result, err := h.pulse.GetUsage(c.Request.Context(), identity, refresh)
	if err != nil {
		c.JSON(http.StatusOK, pulseErrorResponse{Source: sourceFromRefresh(refresh), Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *PulseHandler) disabled() bool {
	return h == nil || h.cfg == nil || len(h.cfg.Pulse.AccessTokens) == 0
}

func (h *PulseHandler) identity(c *gin.Context) (*config.PulseAccessTokenConfig, bool) {
	if h.disabled() {
		return nil, false
	}
	token := pulseTokenFromHeaders(c)
	if token == "" {
		return nil, false
	}
	return h.cfg.ResolvePulseIdentity(token)
}

type pulseErrorResponse struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

func pulseTokenFromHeaders(c *gin.Context) string {
	if token := strings.TrimSpace(c.GetHeader("X-Pulse-Token")); token != "" {
		return token
	}
	const bearerPrefix = "Bearer "
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(auth, bearerPrefix) {
		return strings.TrimSpace(strings.TrimPrefix(auth, bearerPrefix))
	}
	return ""
}

func sourceFromRefresh(refresh bool) string {
	if refresh {
		return "active"
	}
	return "passive"
}
