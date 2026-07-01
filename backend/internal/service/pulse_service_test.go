package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type pulseQuotaSnapshotFake struct {
	snapshot *QuotaSnapshot
}

func (f *pulseQuotaSnapshotFake) Snapshot(ctx context.Context, accountID int64) (*QuotaSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.snapshot == nil {
		return &QuotaSnapshot{AccountID: accountID}, nil
	}
	copy := *f.snapshot
	copy.AccountID = accountID
	return &copy, nil
}

type pulseAccountUsageFake struct {
	passiveUsage    *UsageInfo
	activeUsage     *UsageInfo
	activeCallCount int
}

func (f *pulseAccountUsageFake) GetPassiveUsage(ctx context.Context, _ int64) (*UsageInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.passiveUsage, nil
}

func (f *pulseAccountUsageFake) GetUsage(ctx context.Context, _ int64, force ...bool) (*UsageInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(force) > 0 && force[0] {
		f.activeCallCount++
	}
	return f.activeUsage, nil
}

type pulseUsageStatsFake struct {
	accountStats *usagestats.UsageStats
	guestStats   GuestOwnStats
}

func (f *pulseUsageStatsFake) GetAccountStatsAggregated(ctx context.Context, _ int64, _, _ time.Time) (*usagestats.UsageStats, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.accountStats, nil
}

func (f *pulseUsageStatsFake) GuestOwnWindowStats(ctx context.Context, _ int64, _, _ time.Time) (GuestOwnStats, error) {
	if err := ctx.Err(); err != nil {
		return GuestOwnStats{}, err
	}
	return f.guestStats, nil
}

func TestPulseOwnerResponseIncludesRealFields(t *testing.T) {
	// Given
	resetAt := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	updatedAt := resetAt.Add(-time.Hour)
	svc := newPulseServiceWithDependencies(
		&config.Config{},
		PulseServiceDependencies{
			AccountUsage: &pulseAccountUsageFake{passiveUsage: &UsageInfo{Source: "passive", UpdatedAt: &updatedAt, FiveHour: &UsageProgress{Utilization: 20}, SevenDay: &UsageProgress{Utilization: 70, ResetsAt: &resetAt, RemainingSeconds: 3600}}},
			Quota:        &pulseQuotaSnapshotFake{snapshot: &QuotaSnapshot{EffectiveBudgetUSD: 1000, GuestCapUSD: 660, OwnerCapUSD: 340, GuestSettledUSD: 200, GuestReservedUSD: 20, OwnerSettledUSD: 400, OwnerReservedUSD: 10, OwnerOverflowUSD: 70, GuestDepletionUSD: 290, U7d: 0.7, ResetsAt: &resetAt, RemainingSeconds: 3600}},
			UsageStats:   &pulseUsageStatsFake{accountStats: &usagestats.UsageStats{TotalRequests: 5, TotalInputTokens: 100, TotalOutputTokens: 50, TotalCacheTokens: 25, TotalTokens: 175}},
		},
	)
	identity := &config.PulseAccessTokenConfig{Role: config.PulseAccessRoleOwner, AccountID: 9, Label: "owner-a"}

	// When
	dto, err := svc.GetUsage(context.Background(), identity, false)

	// Then
	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	owner, ok := dto.(PulseOwnerUsageDTO)
	if !ok {
		t.Fatalf("GetUsage() type = %T, want PulseOwnerUsageDTO", dto)
	}
	if owner.EffectiveBudgetUSD != 1000 || owner.GuestCapUSD != 660 || owner.OwnerCapUSD != 340 || owner.GuestDepletionUSD != 290 || owner.U7d != 0.7 {
		t.Fatalf("owner quota fields = %#v, want real quota values", owner)
	}
	if owner.SevenDay == nil || owner.SevenDay.Utilization != 70 || owner.TokenStats == nil || owner.TokenStats.CacheTokens != 25 {
		t.Fatalf("owner usage/token stats = %#v", owner)
	}
}

func TestPulseGuestResponseUsesScaledGauge(t *testing.T) {
	// Given
	resetAt := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	svc := newPulseServiceWithDependencies(
		&config.Config{},
		PulseServiceDependencies{
			AccountUsage: &pulseAccountUsageFake{passiveUsage: &UsageInfo{Source: "passive", SevenDay: &UsageProgress{ResetsAt: &resetAt, RemainingSeconds: 3600}}},
			Quota:        &pulseQuotaSnapshotFake{snapshot: &QuotaSnapshot{GuestCapUSD: 660, GuestDepletionUSD: 330, ResetsAt: &resetAt, RemainingSeconds: 3600}},
			UsageStats:   &pulseUsageStatsFake{guestStats: GuestOwnStats{RequestCount: 3, InputTokens: 100, OutputTokens: 50}},
		},
	)
	identity := &config.PulseAccessTokenConfig{Role: config.PulseAccessRoleGuest, AccountID: 9, APIKeyID: 23, Label: "guest-c"}

	// When
	dto, err := svc.GetUsage(context.Background(), identity, false)

	// Then
	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	guest, ok := dto.(PulseGuestUsageDTO)
	if !ok {
		t.Fatalf("GetUsage() type = %T, want PulseGuestUsageDTO", dto)
	}
	if guest.WeeklyUsedPercent != 50 || guest.WeeklyRemainingPercent != 50 {
		t.Fatalf("weekly percents = %d/%d, want 50/50", guest.WeeklyUsedPercent, guest.WeeklyRemainingPercent)
	}
}

func TestPulseGuestResponseOmitsSensitive(t *testing.T) {
	// Given
	resetAt := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	svc := newPulseServiceWithDependencies(
		&config.Config{},
		PulseServiceDependencies{
			AccountUsage: &pulseAccountUsageFake{passiveUsage: &UsageInfo{Source: "passive", SevenDay: &UsageProgress{Utilization: 70, ResetsAt: &resetAt, RemainingSeconds: 3600}}},
			Quota:        &pulseQuotaSnapshotFake{snapshot: &QuotaSnapshot{EffectiveBudgetUSD: 1000, GuestCapUSD: 660, OwnerCapUSD: 340, GuestDepletionUSD: 330, OwnerOverflowUSD: 60, U7d: 0.7, ResetsAt: &resetAt, RemainingSeconds: 3600}},
			UsageStats:   &pulseUsageStatsFake{accountStats: &usagestats.UsageStats{TotalCacheTokens: 999}, guestStats: GuestOwnStats{RequestCount: 3, InputTokens: 100, OutputTokens: 50}},
		},
	)
	identity := &config.PulseAccessTokenConfig{Role: config.PulseAccessRoleGuest, AccountID: 9, APIKeyID: 23, Label: "guest-c"}

	// When
	dto, err := svc.GetUsage(context.Background(), identity, false)
	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	body, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal guest dto: %v", err)
	}

	// Then
	lowerJSON := strings.ToLower(string(body))
	for _, forbidden := range []string{"usd", "budget", "cost", "cache", "utilization", "u7d", "owner", "effective", "cap", "depletion"} {
		if strings.Contains(lowerJSON, forbidden) {
			t.Fatalf("guest DTO leaked %q in %s", forbidden, string(body))
		}
	}
	for _, required := range []string{"weekly_used_percent", "input_tokens", "output_tokens", "request_count"} {
		if !strings.Contains(lowerJSON, required) {
			t.Fatalf("guest DTO missing %q in %s", required, string(body))
		}
	}
}

func TestPulseGuestRefreshActiveNotAllowed(t *testing.T) {
	// Given
	resetAt := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	accountUsage := &pulseAccountUsageFake{passiveUsage: &UsageInfo{Source: "passive"}, activeUsage: &UsageInfo{Source: "active"}}
	svc := newPulseServiceWithDependencies(
		&config.Config{},
		PulseServiceDependencies{
			AccountUsage: accountUsage,
			Quota:        &pulseQuotaSnapshotFake{snapshot: &QuotaSnapshot{GuestCapUSD: 660, GuestDepletionUSD: 330, ResetsAt: &resetAt, RemainingSeconds: 3600}},
			UsageStats:   &pulseUsageStatsFake{guestStats: GuestOwnStats{RequestCount: 3}},
		},
	)
	identity := &config.PulseAccessTokenConfig{Role: config.PulseAccessRoleGuest, AccountID: 9, APIKeyID: 23, Label: "guest-c"}

	// When
	_, err := svc.GetUsage(context.Background(), identity, true)

	// Then
	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	if accountUsage.activeCallCount != 0 {
		t.Fatalf("activeCallCount = %d, want 0 for guest refresh", accountUsage.activeCallCount)
	}
}
