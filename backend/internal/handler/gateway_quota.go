package handler

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const weeklyQuotaExhaustedMessage = "账号周限额已满"

type weeklyQuotaGatewayService interface {
	Reserve(ctx context.Context, request service.ReserveRequest) (service.ReserveDecision, error)
	Settle(ctx context.Context, decision service.ReserveDecision, actualCostUSD float64) error
	Release(ctx context.Context, decision service.ReserveDecision) error
}

type weeklyQuotaAttempt struct {
	reservation *quotaReservation
	decision    service.ReserveDecision
	retry       bool
	reject      bool
}

type quotaReservation struct {
	service  weeklyQuotaGatewayService
	decision service.ReserveDecision
	closed   bool
}

func newQuotaReservation(service weeklyQuotaGatewayService, decision service.ReserveDecision) *quotaReservation {
	if service == nil || !decision.Managed || !decision.Admitted {
		return nil
	}
	return &quotaReservation{service: service, decision: decision}
}

func (r *quotaReservation) settle(ctx context.Context, actualCostUSD float64) error {
	if r == nil || r.closed {
		return nil
	}
	r.closed = true
	return r.service.Settle(ctx, r.decision, actualCostUSD)
}

func (r *quotaReservation) release(ctx context.Context) error {
	if r == nil || r.closed {
		return nil
	}
	r.closed = true
	return r.service.Release(ctx, r.decision)
}

func (h *GatewayHandler) reserveWeeklyQuota(ctx context.Context, accountID int64, apiKeyID int64) (service.ReserveDecision, error) {
	if h.weeklyQuotaService == nil {
		return service.ReserveDecision{Managed: false, Admitted: true, AccountID: accountID}, nil
	}
	return h.weeklyQuotaService.Reserve(ctx, service.ReserveRequest{
		AccountID:        accountID,
		APIKeyID:         apiKeyID,
		RequestID:        weeklyQuotaRequestID(ctx),
		EstimatedCostUSD: 0,
	})
}

func (h *GatewayHandler) prepareWeeklyQuotaAttempt(ctx context.Context, accountID int64, apiKeyID int64, failedAccountIDs map[int64]struct{}) (weeklyQuotaAttempt, error) {
	decision, err := h.reserveWeeklyQuota(ctx, accountID, apiKeyID)
	if err != nil {
		return weeklyQuotaAttempt{}, err
	}
	if decision.OwnerSafetyLineHit() {
		failedAccountIDs[accountID] = struct{}{}
		return weeklyQuotaAttempt{decision: decision, retry: true}, nil
	}
	if !decision.Admitted {
		return weeklyQuotaAttempt{decision: decision, reject: true}, nil
	}
	return weeklyQuotaAttempt{decision: decision, reservation: newQuotaReservation(h.weeklyQuotaService, decision)}, nil
}

func weeklyQuotaRequestID(ctx context.Context) string {
	if ctx != nil {
		if clientRequestID, _ := ctx.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(clientRequestID) != "" {
			return "client:" + strings.TrimSpace(clientRequestID)
		}
		if requestID, _ := ctx.Value(ctxkey.RequestID).(string); strings.TrimSpace(requestID) != "" {
			return "local:" + strings.TrimSpace(requestID)
		}
	}
	return ""
}

func (h *GatewayHandler) handleWeeklyQuotaExhausted(c *gin.Context, decision service.ReserveDecision, streamStarted bool) {
	if decision.RemainingSeconds > 0 {
		c.Header("Retry-After", strconv.Itoa(decision.RemainingSeconds))
	}
	if !streamStarted {
		c.JSON(429, gin.H{
			"error": gin.H{
				"type":    "rate_limit_exceeded",
				"message": weeklyQuotaExhaustedMessage,
			},
		})
		return
	}
	h.handleStreamingAwareError(c, 429, "rate_limit_exceeded", weeklyQuotaExhaustedMessage, streamStarted)
}
