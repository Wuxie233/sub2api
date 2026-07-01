package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type PulseService struct {
	cfg          *config.Config
	accountUsage pulseAccountUsageProvider
	quota        pulseQuotaSnapshotProvider
	usageStats   pulseUsageStatsProvider
}

func NewPulseService(cfg *config.Config, usageService *AccountUsageService, weeklyQuota *WeeklyQuotaService) *PulseService {
	return newPulseServiceWithDependencies(cfg, PulseServiceDependencies{
		AccountUsage: usageService,
		Quota:        weeklyQuota,
		UsageStats:   usageServiceUsageStats{usageService: usageService},
	})
}

func newPulseServiceWithDependencies(cfg *config.Config, deps PulseServiceDependencies) *PulseService {
	return &PulseService{
		cfg:          cfg,
		accountUsage: deps.AccountUsage,
		quota:        deps.Quota,
		usageStats:   deps.UsageStats,
	}
}

func (s *PulseService) GetUsage(ctx context.Context, identity *config.PulseAccessTokenConfig, refresh bool) (PulseUsageDTO, error) {
	if identity == nil {
		return nil, fmt.Errorf("pulse identity is required")
	}
	snapshot, err := s.snapshot(ctx, identity.AccountID)
	if err != nil {
		return nil, err
	}
	switch identity.Role {
	case config.PulseAccessRoleOwner:
		return s.ownerUsage(ctx, identity, snapshot, refresh)
	case config.PulseAccessRoleGuest:
		return s.guestUsage(ctx, identity, snapshot)
	default:
		return nil, fmt.Errorf("unsupported pulse role %q", identity.Role)
	}
}

func (s *PulseService) ownerUsage(ctx context.Context, identity *config.PulseAccessTokenConfig, snapshot *QuotaSnapshot, refresh bool) (PulseOwnerUsageDTO, error) {
	usage, err := s.accountUsageForOwner(ctx, identity.AccountID, refresh)
	if err != nil {
		return PulseOwnerUsageDTO{}, err
	}
	stats, err := s.accountStats(ctx, identity.AccountID, windowStart(snapshot), time.Now())
	if err != nil {
		return PulseOwnerUsageDTO{}, err
	}
	return PulseOwnerUsageDTO{
		Role:               identity.Role,
		Label:              identity.Label,
		Source:             usageSource(usage, refresh),
		UpdatedAt:          usageUpdatedAt(usage),
		FiveHour:           newPulseWindowUsageDTO(usageFiveHour(usage)),
		SevenDay:           newPulseWindowUsageDTO(usageSevenDay(usage)),
		ResetsAt:           snapshot.ResetsAt,
		RemainingSeconds:   snapshot.RemainingSeconds,
		EffectiveBudgetUSD: snapshot.EffectiveBudgetUSD,
		GuestCapUSD:        snapshot.GuestCapUSD,
		OwnerCapUSD:        snapshot.OwnerCapUSD,
		GuestSettledUSD:    snapshot.GuestSettledUSD,
		GuestReservedUSD:   snapshot.GuestReservedUSD,
		OwnerSettledUSD:    snapshot.OwnerSettledUSD,
		OwnerReservedUSD:   snapshot.OwnerReservedUSD,
		OwnerOverflowUSD:   snapshot.OwnerOverflowUSD,
		GuestDepletionUSD:  snapshot.GuestDepletionUSD,
		U7d:                snapshot.U7d,
		TokenStats:         newPulseTokenStatsDTO(stats),
	}, nil
}

func (s *PulseService) guestUsage(ctx context.Context, identity *config.PulseAccessTokenConfig, snapshot *QuotaSnapshot) (PulseGuestUsageDTO, error) {
	stats, err := s.guestStats(ctx, identity.APIKeyID, windowStart(snapshot), time.Now())
	if err != nil {
		return PulseGuestUsageDTO{}, err
	}
	usedPercent := scaledPercent(snapshot)
	return PulseGuestUsageDTO{
		Role:                   identity.Role,
		Label:                  identity.Label,
		WeeklyUsedPercent:      usedPercent,
		WeeklyRemainingPercent: 100 - usedPercent,
		ResetsAt:               snapshot.ResetsAt,
		RemainingSeconds:       snapshot.RemainingSeconds,
		InputTokens:            stats.InputTokens,
		OutputTokens:           stats.OutputTokens,
		RequestCount:           stats.RequestCount,
	}, nil
}

func (s *PulseService) snapshot(ctx context.Context, accountID int64) (*QuotaSnapshot, error) {
	if s.quota == nil {
		return &QuotaSnapshot{AccountID: accountID}, nil
	}
	snapshot, err := s.quota.Snapshot(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get pulse quota snapshot: %w", err)
	}
	if snapshot == nil {
		return &QuotaSnapshot{AccountID: accountID}, nil
	}
	return snapshot, nil
}

func (s *PulseService) accountUsageForOwner(ctx context.Context, accountID int64, refresh bool) (*UsageInfo, error) {
	if s.accountUsage == nil {
		return &UsageInfo{}, nil
	}
	if refresh {
		usage, err := s.accountUsage.GetUsage(ctx, accountID, true)
		if err != nil {
			return nil, fmt.Errorf("get active pulse usage: %w", err)
		}
		if usage != nil {
			usage.Source = "active"
		}
		return usage, nil
	}
	usage, err := s.accountUsage.GetPassiveUsage(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get passive pulse usage: %w", err)
	}
	return usage, nil
}

func (s *PulseService) accountStats(ctx context.Context, accountID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	if s.usageStats == nil {
		return nil, nil
	}
	stats, err := s.usageStats.GetAccountStatsAggregated(ctx, accountID, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get pulse account stats: %w", err)
	}
	return stats, nil
}

func (s *PulseService) guestStats(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) (GuestOwnStats, error) {
	if s.usageStats == nil {
		return GuestOwnStats{}, nil
	}
	stats, err := s.usageStats.GuestOwnWindowStats(ctx, apiKeyID, startTime, endTime)
	if err != nil {
		return GuestOwnStats{}, fmt.Errorf("get pulse guest stats: %w", err)
	}
	return stats, nil
}
