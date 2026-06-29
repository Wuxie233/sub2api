package handler

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/web/pulse"
	"github.com/gin-gonic/gin"
)

type PulseUsageProvider interface {
	GetUsage(ctx context.Context, refresh bool) (*service.PulseUsageResult, error)
}

type PulseHandler struct {
	cfg   *config.Config
	pulse PulseUsageProvider
}

func NewPulseHandler(cfg *config.Config, pulseService PulseUsageProvider) *PulseHandler {
	return &PulseHandler{cfg: cfg, pulse: pulseService}
}

func (h *PulseHandler) ServePage(c *gin.Context) {
	if !h.authorized(c) {
		if h.disabled() {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusUnauthorized)
		return
	}
	nonce := middleware.GetNonceFromContext(c)
	html := bytes.ReplaceAll(pulse.DashboardHTML, []byte(pulse.NoncePlaceholder), []byte(nonce))
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}

func (h *PulseHandler) ServeUsage(c *gin.Context) {
	if !h.authorized(c) {
		if h.disabled() {
			c.Status(http.StatusNotFound)
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	result, err := h.pulse.GetUsage(c.Request.Context(), c.Query("refresh") == "1")
	if err != nil {
		c.JSON(http.StatusOK, pulseUsageResponse{
			Source:   sourceFromRefresh(c.Query("refresh") == "1"),
			FiveHour: nil,
			SevenDay: nil,
			Error:    err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, newPulseUsageResponse(result))
}

func (h *PulseHandler) disabled() bool {
	return h == nil || h.cfg == nil || strings.TrimSpace(h.cfg.Pulse.Token) == ""
}

func (h *PulseHandler) authorized(c *gin.Context) bool {
	if h.disabled() {
		return false
	}
	token := h.cfg.Pulse.Token
	return c.GetHeader("X-Pulse-Token") == token || c.Query("token") == token
}

type pulseUsageResponse struct {
	AccountLabel string            `json:"account_label"`
	Source       string            `json:"source"`
	UpdatedAt    *time.Time        `json:"updated_at"`
	FiveHour     *pulseWindowUsage `json:"five_hour"`
	SevenDay     *pulseWindowUsage `json:"seven_day"`
	Error        string            `json:"error,omitempty"`
}

type pulseWindowUsage struct {
	Utilization      float64    `json:"utilization"`
	ResetsAt         *time.Time `json:"resets_at"`
	RemainingSeconds int        `json:"remaining_seconds"`
}

func newPulseUsageResponse(result *service.PulseUsageResult) pulseUsageResponse {
	if result == nil || result.Usage == nil {
		return pulseUsageResponse{Source: "passive"}
	}
	return pulseUsageResponse{
		AccountLabel: result.AccountLabel,
		Source:       result.Usage.Source,
		UpdatedAt:    result.Usage.UpdatedAt,
		FiveHour:     newPulseWindowUsage(result.Usage.FiveHour),
		SevenDay:     newPulseWindowUsage(result.Usage.SevenDay),
	}
}

func newPulseWindowUsage(progress *service.UsageProgress) *pulseWindowUsage {
	if progress == nil {
		return nil
	}
	return &pulseWindowUsage{
		Utilization:      progress.Utilization,
		ResetsAt:         progress.ResetsAt,
		RemainingSeconds: progress.RemainingSeconds,
	}
}

func sourceFromRefresh(refresh bool) string {
	if refresh {
		return "active"
	}
	return "passive"
}
