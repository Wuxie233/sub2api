package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fakeWeeklyQuotaGatewayService struct {
	reserveDecision service.ReserveDecision
	reserveRequest  service.ReserveRequest
	settled         []service.ReserveDecision
	released        []service.ReserveDecision
}

func (f *fakeWeeklyQuotaGatewayService) Reserve(_ context.Context, request service.ReserveRequest) (service.ReserveDecision, error) {
	f.reserveRequest = request
	return f.reserveDecision, nil
}

func (f *fakeWeeklyQuotaGatewayService) Settle(_ context.Context, decision service.ReserveDecision, _ float64) error {
	f.settled = append(f.settled, decision)
	return nil
}

func (f *fakeWeeklyQuotaGatewayService) Release(_ context.Context, decision service.ReserveDecision) error {
	f.released = append(f.released, decision)
	return nil
}

func TestGatewayWeeklyQuota_PrepareRetriesOwnerWhenSafetyLineHit(t *testing.T) {
	// Given
	quota := &fakeWeeklyQuotaGatewayService{reserveDecision: service.ReserveDecision{
		Managed:   true,
		Admitted:  false,
		Role:      service.WeeklyQuotaRoleOwner,
		Reason:    service.WeeklyQuotaReasonOwnerSafetyLine,
		AccountID: 9,
	}}
	h := &GatewayHandler{weeklyQuotaService: quota}
	failedAccountIDs := map[int64]struct{}{}

	// When
	attempt, err := h.prepareWeeklyQuotaAttempt(context.Background(), 9, 1, failedAccountIDs)

	// Then
	require.NoError(t, err)
	require.True(t, attempt.retry)
	require.False(t, attempt.reject)
	require.Nil(t, attempt.reservation)
	require.Contains(t, failedAccountIDs, int64(9))
}

func TestGatewayWeeklyQuota_PrepareRejectsGuestWhenPoolExhausted(t *testing.T) {
	// Given
	quota := &fakeWeeklyQuotaGatewayService{reserveDecision: service.ReserveDecision{
		Managed:          true,
		Admitted:         false,
		Role:             service.WeeklyQuotaRoleGuest,
		Reason:           service.WeeklyQuotaReasonGuestPoolFull,
		AccountID:        9,
		RemainingSeconds: 321,
	}}
	h := &GatewayHandler{weeklyQuotaService: quota}

	// When
	attempt, err := h.prepareWeeklyQuotaAttempt(context.Background(), 9, 23, map[int64]struct{}{})

	// Then
	require.NoError(t, err)
	require.False(t, attempt.retry)
	require.True(t, attempt.reject)
	require.Nil(t, attempt.reservation)
	require.Equal(t, 321, attempt.decision.RemainingSeconds)
}

func TestGatewayWeeklyQuota_ReservationClosesExactlyOnce(t *testing.T) {
	// Given
	decision := service.ReserveDecision{
		Managed:    true,
		Admitted:   true,
		Role:       service.WeeklyQuotaRoleGuest,
		AccountID:  9,
		ResetEpoch: 1700000000,
		RequestID:  "req_1",
	}
	quota := &fakeWeeklyQuotaGatewayService{reserveDecision: decision}
	reservation := newQuotaReservation(quota, decision)

	// When
	require.NoError(t, reservation.settle(context.Background(), 0.42))
	require.NoError(t, reservation.release(context.Background()))
	require.NoError(t, reservation.settle(context.Background(), 0.84))

	// Then
	require.Len(t, quota.settled, 1)
	require.Len(t, quota.released, 0)
}

func TestGatewayWeeklyQuota_RequestIDPrefersClientRequestID(t *testing.T) {
	// Given
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, "local-1")
	ctx = context.WithValue(ctx, ctxkey.ClientRequestID, "client-1")
	quota := &fakeWeeklyQuotaGatewayService{reserveDecision: service.ReserveDecision{Managed: false, Admitted: true}}
	h := &GatewayHandler{weeklyQuotaService: quota}

	// When
	_, err := h.reserveWeeklyQuota(ctx, 9, 23)

	// Then
	require.NoError(t, err)
	require.Equal(t, "client:client-1", quota.reserveRequest.RequestID)
}

func TestGatewayWeeklyQuota_HandleRejectWrites429Surface(t *testing.T) {
	// Given
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	decision := service.ReserveDecision{
		Managed:          true,
		Admitted:         false,
		Role:             service.WeeklyQuotaRoleGuest,
		Reason:           service.WeeklyQuotaReasonGuestPoolFull,
		RemainingSeconds: 123,
	}

	// When
	(&GatewayHandler{}).handleWeeklyQuotaExhausted(c, decision, false)

	// Then
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "123", recorder.Header().Get("Retry-After"))
	require.JSONEq(t, `{"error":{"type":"rate_limit_exceeded","message":"账号周限额已满"}}`, recorder.Body.String())
}
