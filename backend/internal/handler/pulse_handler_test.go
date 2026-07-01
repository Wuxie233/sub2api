package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type pulseServiceFake struct {
	result       service.PulseUsageDTO
	err          error
	refreshCalls []bool
	identity     *config.PulseAccessTokenConfig
}

func (f *pulseServiceFake) GetUsage(ctx context.Context, identity *config.PulseAccessTokenConfig, refresh bool) (service.PulseUsageDTO, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.refreshCalls = append(f.refreshCalls, refresh)
	f.identity = identity
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func setupPulseRouter(tokens []config.PulseAccessTokenConfig, fake *pulseServiceFake) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewPulseHandler(&config.Config{Pulse: config.PulseConfig{AccessTokens: tokens}}, fake)
	router.GET("/pulse", h.ServePage)
	router.GET("/pulse/api/usage", h.ServeUsage)
	return router
}

func TestPulsePageServesWhenEnabledWithoutToken(t *testing.T) {
	// Given
	router := setupPulseRouter([]config.PulseAccessTokenConfig{ownerPulseTokenFixture("owner-token")}, &pulseServiceFake{})

	// When
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pulse", nil)
	router.ServeHTTP(rec, req)

	// Then
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /pulse status = %d, want 200", rec.Code)
	}
}

func TestPulsePageDisabledReturns404(t *testing.T) {
	// Given
	router := setupPulseRouter(nil, &pulseServiceFake{})

	// When
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pulse", nil)
	router.ServeHTTP(rec, req)

	// Then
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /pulse status = %d, want 404", rec.Code)
	}
}

func TestPulseUsageDisabledReturns404(t *testing.T) {
	// Given
	router := setupPulseRouter(nil, &pulseServiceFake{})

	// When
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pulse/api/usage", nil)
	router.ServeHTTP(rec, req)

	// Then
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /pulse/api/usage status = %d, want 404", rec.Code)
	}
}

func TestPulseUsageRejectsMissingOrBadToken(t *testing.T) {
	// Given
	router := setupPulseRouter([]config.PulseAccessTokenConfig{ownerPulseTokenFixture("owner-token")}, &pulseServiceFake{})

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/pulse/api/usage", nil),
		httptest.NewRequest(http.MethodGet, "/pulse/api/usage?token=owner-token", nil),
		requestWithHeader("/pulse/api/usage", "X-Pulse-Token", "wrong-token"),
		requestWithHeader("/pulse/api/usage", "Authorization", "Bearer wrong-token"),
	} {
		// When
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		// Then
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s status = %d, want 401", req.URL.String(), rec.Code)
		}
	}
}

func TestPulseUsageAcceptsBearerAndXPulseToken(t *testing.T) {
	// Given
	fake := &pulseServiceFake{result: service.PulseGuestUsageDTO{Role: "guest", Label: "guest-c", WeeklyUsedPercent: 10}}
	router := setupPulseRouter([]config.PulseAccessTokenConfig{guestPulseTokenFixture("guest-token")}, fake)

	for _, req := range []*http.Request{
		requestWithHeader("/pulse/api/usage", "Authorization", "Bearer guest-token"),
		requestWithHeader("/pulse/api/usage", "X-Pulse-Token", "guest-token"),
	} {
		// When
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		// Then
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", req.URL.String(), rec.Code)
		}
	}
}

func TestPulseTokenHashConstantTime(t *testing.T) {
	// Given
	router := setupPulseRouter([]config.PulseAccessTokenConfig{ownerPulseTokenFixture("owner-token")}, &pulseServiceFake{})

	// When
	rec := httptest.NewRecorder()
	req := requestWithHeader("/pulse/api/usage", "Authorization", "Bearer wrong-token")
	router.ServeHTTP(rec, req)

	// Then
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["error"] != "unauthorized" {
		t.Fatalf("error = %#v, want unauthorized", body["error"])
	}
}

func TestPulseOwnerResponseIncludesRealFields(t *testing.T) {
	// Given
	updatedAt := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	resetAt := updatedAt.Add(2 * time.Hour)
	fake := &pulseServiceFake{result: service.PulseOwnerUsageDTO{
		Role:               "owner",
		Label:              "owner-a",
		Source:             "passive",
		UpdatedAt:          &updatedAt,
		FiveHour:           &service.PulseWindowUsageDTO{Utilization: 25.5, ResetsAt: &resetAt, RemainingSeconds: 7200},
		SevenDay:           &service.PulseWindowUsageDTO{Utilization: 70, RemainingSeconds: 0},
		EffectiveBudgetUSD: 1000,
		GuestCapUSD:        660,
		OwnerCapUSD:        340,
		GuestDepletionUSD:  330,
		U7d:                0.7,
		TokenStats:         &service.PulseTokenStatsDTO{RequestCount: 5, InputTokens: 100, OutputTokens: 50, CacheTokens: 25, TotalTokens: 175},
	}}
	router := setupPulseRouter([]config.PulseAccessTokenConfig{ownerPulseTokenFixture("owner-token")}, fake)

	// When
	rec := httptest.NewRecorder()
	req := requestWithHeader("/pulse/api/usage", "Authorization", "Bearer owner-token")
	router.ServeHTTP(rec, req)

	// Then
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	for _, key := range []string{"effective_budget_usd", "guest_cap_usd", "owner_cap_usd", "guest_depletion_usd", "u7d", "token_stats", "seven_day", "five_hour"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("owner response missing real field %q in %#v", key, body)
		}
	}
	if body["role"] != "owner" || body["label"] != "owner-a" {
		t.Fatalf("unexpected role/label in %#v", body)
	}
}

func TestPulseGuestResponseOmitsSensitive(t *testing.T) {
	// Given
	fake := &pulseServiceFake{result: service.PulseGuestUsageDTO{Role: "guest", Label: "guest-c", WeeklyUsedPercent: 50, WeeklyRemainingPercent: 50, InputTokens: 100, OutputTokens: 50, RequestCount: 3}}
	router := setupPulseRouter([]config.PulseAccessTokenConfig{guestPulseTokenFixture("guest-token")}, fake)

	// When
	rec := httptest.NewRecorder()
	req := requestWithHeader("/pulse/api/usage", "X-Pulse-Token", "guest-token")
	router.ServeHTTP(rec, req)

	// Then
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	lowerJSON := strings.ToLower(rec.Body.String())
	for _, forbidden := range []string{"usd", "budget", "cost", "cache", "utilization", "u7d", "owner", "effective", "cap", "depletion"} {
		if strings.Contains(lowerJSON, forbidden) {
			t.Fatalf("guest response leaked %q in %s", forbidden, rec.Body.String())
		}
	}
	for _, required := range []string{"weekly_used_percent", "input_tokens", "output_tokens", "request_count"} {
		if !strings.Contains(lowerJSON, required) {
			t.Fatalf("guest response missing %q in %s", required, rec.Body.String())
		}
	}
}

func TestPulseGuestRefreshActiveNotAllowed(t *testing.T) {
	// Given
	fake := &pulseServiceFake{result: service.PulseGuestUsageDTO{Role: "guest", Label: "guest-c", WeeklyUsedPercent: 50}}
	router := setupPulseRouter([]config.PulseAccessTokenConfig{guestPulseTokenFixture("guest-token")}, fake)

	// When
	rec := httptest.NewRecorder()
	req := requestWithHeader("/pulse/api/usage?refresh=1", "X-Pulse-Token", "guest-token")
	router.ServeHTTP(rec, req)

	// Then
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(fake.refreshCalls) != 1 || fake.refreshCalls[0] {
		t.Fatalf("refresh calls = %#v, want [false]", fake.refreshCalls)
	}
}

func TestPulseHandler_ServiceErrorSoftDegrades(t *testing.T) {
	// Given
	router := setupPulseRouter([]config.PulseAccessTokenConfig{ownerPulseTokenFixture("owner-token")}, &pulseServiceFake{err: errors.New("upstream unavailable")})

	// When
	rec := httptest.NewRecorder()
	req := requestWithHeader("/pulse/api/usage", "X-Pulse-Token", "owner-token")
	router.ServeHTTP(rec, req)

	// Then
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["error"] != "upstream unavailable" {
		t.Fatalf("error = %#v, want service error", body["error"])
	}
	if body["source"] != "passive" {
		t.Fatalf("source = %#v, want passive", body["source"])
	}
}

func ownerPulseTokenFixture(rawToken string) config.PulseAccessTokenConfig {
	return pulseTokenFixture(rawToken, config.PulseAccessRoleOwner, 9, 0, "owner-a")
}

func guestPulseTokenFixture(rawToken string) config.PulseAccessTokenConfig {
	return pulseTokenFixture(rawToken, config.PulseAccessRoleGuest, 9, 23, "guest-c")
}

func pulseTokenFixture(rawToken string, role string, accountID int64, apiKeyID int64, label string) config.PulseAccessTokenConfig {
	hash := sha256.Sum256([]byte(rawToken))
	return config.PulseAccessTokenConfig{TokenSHA256: fmt.Sprintf("%x", hash[:]), Role: role, AccountID: accountID, APIKeyID: apiKeyID, Label: label}
}

func requestWithHeader(path string, headerName string, headerValue string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set(headerName, headerValue)
	return req
}
