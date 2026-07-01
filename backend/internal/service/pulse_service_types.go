package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type PulseUsageDTO interface {
	pulseUsageDTO()
}

type PulseWindowUsageDTO struct {
	Utilization      float64    `json:"utilization"`
	ResetsAt         *time.Time `json:"resets_at"`
	RemainingSeconds int        `json:"remaining_seconds"`
}

type PulseTokenStatsDTO struct {
	RequestCount int64 `json:"request_count"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CacheTokens  int64 `json:"cache_tokens,omitempty"`
	TotalTokens  int64 `json:"total_tokens,omitempty"`
}

type PulseOwnerUsageDTO struct {
	Role               string               `json:"role"`
	Label              string               `json:"label"`
	Source             string               `json:"source"`
	UpdatedAt          *time.Time           `json:"updated_at,omitempty"`
	FiveHour           *PulseWindowUsageDTO `json:"five_hour,omitempty"`
	SevenDay           *PulseWindowUsageDTO `json:"seven_day,omitempty"`
	ResetsAt           *time.Time           `json:"resets_at,omitempty"`
	RemainingSeconds   int                  `json:"remaining_seconds"`
	EffectiveBudgetUSD float64              `json:"effective_budget_usd"`
	GuestCapUSD        float64              `json:"guest_cap_usd"`
	OwnerCapUSD        float64              `json:"owner_cap_usd"`
	GuestSettledUSD    float64              `json:"guest_settled_usd"`
	GuestReservedUSD   float64              `json:"guest_reserved_usd"`
	OwnerSettledUSD    float64              `json:"owner_settled_usd"`
	OwnerReservedUSD   float64              `json:"owner_reserved_usd"`
	OwnerOverflowUSD   float64              `json:"owner_overflow_usd"`
	GuestDepletionUSD  float64              `json:"guest_depletion_usd"`
	U7d                float64              `json:"u7d"`
	TokenStats         *PulseTokenStatsDTO  `json:"token_stats,omitempty"`
}

func (PulseOwnerUsageDTO) pulseUsageDTO() {}

type PulseGuestUsageDTO struct {
	Role                   string     `json:"role"`
	Label                  string     `json:"label"`
	WeeklyUsedPercent      int        `json:"weekly_used_percent"`
	WeeklyRemainingPercent int        `json:"weekly_remaining_percent"`
	ResetsAt               *time.Time `json:"resets_at,omitempty"`
	RemainingSeconds       int        `json:"remaining_seconds"`
	InputTokens            int64      `json:"input_tokens"`
	OutputTokens           int64      `json:"output_tokens"`
	RequestCount           int64      `json:"request_count"`
}

func (PulseGuestUsageDTO) pulseUsageDTO() {}

type pulseQuotaSnapshotProvider interface {
	Snapshot(ctx context.Context, accountID int64) (*QuotaSnapshot, error)
}

type pulseAccountUsageProvider interface {
	GetPassiveUsage(ctx context.Context, accountID int64) (*UsageInfo, error)
	GetUsage(ctx context.Context, accountID int64, force ...bool) (*UsageInfo, error)
}

type pulseUsageStatsProvider interface {
	GetAccountStatsAggregated(ctx context.Context, accountID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error)
	GuestOwnWindowStats(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) (GuestOwnStats, error)
}

type PulseServiceDependencies struct {
	AccountUsage pulseAccountUsageProvider
	Quota        pulseQuotaSnapshotProvider
	UsageStats   pulseUsageStatsProvider
}

type usageServiceUsageStats struct {
	usageService *AccountUsageService
}

func (p usageServiceUsageStats) GetAccountStatsAggregated(ctx context.Context, accountID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	if p.usageService == nil || p.usageService.usageLogRepo == nil {
		return nil, nil
	}
	return p.usageService.usageLogRepo.GetAccountStatsAggregated(ctx, accountID, startTime, endTime)
}

func (p usageServiceUsageStats) GuestOwnWindowStats(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) (GuestOwnStats, error) {
	if p.usageService == nil || p.usageService.usageLogRepo == nil {
		return GuestOwnStats{}, nil
	}
	return p.usageService.usageLogRepo.GuestOwnWindowStats(ctx, apiKeyID, startTime, endTime)
}
