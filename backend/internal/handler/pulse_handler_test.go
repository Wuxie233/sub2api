package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type pulseServiceFake struct {
	label        string
	usage        *service.UsageInfo
	err          error
	refreshCalls []bool
}

func (f *pulseServiceFake) GetUsage(ctx context.Context, refresh bool) (*service.PulseUsageResult, error) {
	f.refreshCalls = append(f.refreshCalls, refresh)
	if f.err != nil {
		return nil, f.err
	}
	return &service.PulseUsageResult{AccountLabel: f.label, Usage: f.usage}, nil
}

func setupPulseRouter(token string, fake *pulseServiceFake) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewPulseHandler(&config.Config{Pulse: config.PulseConfig{Token: token}}, fake)
	router.GET("/pulse", h.ServePage)
	router.GET("/pulse/api/usage", h.ServeUsage)
	return router
}

func TestPulseHandler_DisabledReturns404(t *testing.T) {
	router := setupPulseRouter("", &pulseServiceFake{})

	for _, path := range []string{"/pulse", "/pulse/api/usage"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, rec.Code)
		}
	}
}

func TestPulseHandler_TokenRequired(t *testing.T) {
	router := setupPulseRouter("secret", &pulseServiceFake{})

	for _, path := range []string{"/pulse", "/pulse/api/usage"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s without token status = %d, want 401", path, rec.Code)
		}
	}
}

func TestPulseHandler_WrongTokenRejected(t *testing.T) {
	router := setupPulseRouter("secret", &pulseServiceFake{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pulse/api/usage?token=wrong", nil)
	req.Header.Set("X-Pulse-Token", "wrong")
	router.ServeHTTP(rec, req)

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

func TestPulseHandler_HeaderTokenReturnsMinimalUsageJSON(t *testing.T) {
	updatedAt := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	resetAt := updatedAt.Add(2 * time.Hour)
	fake := &pulseServiceFake{
		label: "claude-main",
		usage: &service.UsageInfo{
			Source:    "passive",
			UpdatedAt: &updatedAt,
			FiveHour:  &service.UsageProgress{Utilization: 25.5, ResetsAt: &resetAt, RemainingSeconds: 7200},
			SevenDay:  &service.UsageProgress{Utilization: 70, RemainingSeconds: 0},
		},
	}
	router := setupPulseRouter("secret", fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pulse/api/usage", nil)
	req.Header.Set("X-Pulse-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	wantKeys := map[string]bool{
		"account_label": true,
		"source":        true,
		"updated_at":    true,
		"five_hour":     true,
		"seven_day":     true,
	}
	if len(body) != len(wantKeys) {
		t.Fatalf("response keys = %#v, want only %#v", body, wantKeys)
	}
	for key := range wantKeys {
		if _, ok := body[key]; !ok {
			t.Fatalf("missing key %q in %#v", key, body)
		}
	}
	if body["account_label"] != "claude-main" || body["source"] != "passive" {
		t.Fatalf("unexpected label/source in %#v", body)
	}
	for _, forbidden := range []string{"credentials", "token", "access_token", "refresh_token"} {
		if _, ok := body[forbidden]; ok {
			t.Fatalf("response leaked forbidden field %q: %#v", forbidden, body)
		}
	}
}

func TestPulseHandler_QueryTokenAccepted(t *testing.T) {
	router := setupPulseRouter("secret", &pulseServiceFake{label: "claude", usage: &service.UsageInfo{Source: "passive"}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pulse/api/usage?token=secret", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestPulseHandler_RefreshSelectsActiveMode(t *testing.T) {
	fake := &pulseServiceFake{label: "claude", usage: &service.UsageInfo{Source: "active"}}
	router := setupPulseRouter("secret", fake)

	for _, path := range []string{"/pulse/api/usage?token=secret", "/pulse/api/usage?token=secret&refresh=1"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, rec.Code)
		}
	}
	if len(fake.refreshCalls) != 2 {
		t.Fatalf("refresh call count = %d, want 2", len(fake.refreshCalls))
	}
	if fake.refreshCalls[0] || !fake.refreshCalls[1] {
		t.Fatalf("refresh calls = %#v, want [false true]", fake.refreshCalls)
	}
}

func TestPulseHandler_ServiceErrorSoftDegrades(t *testing.T) {
	router := setupPulseRouter("secret", &pulseServiceFake{err: errors.New("upstream unavailable")})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/pulse/api/usage?token=secret", nil)
	router.ServeHTTP(rec, req)

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
	if body["five_hour"] != nil || body["seven_day"] != nil {
		t.Fatalf("windows = %#v/%#v, want nil on soft error", body["five_hour"], body["seven_day"])
	}
}
