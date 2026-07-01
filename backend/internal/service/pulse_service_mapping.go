package service

import (
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

func newPulseWindowUsageDTO(progress *UsageProgress) *PulseWindowUsageDTO {
	if progress == nil {
		return nil
	}
	return &PulseWindowUsageDTO{Utilization: progress.Utilization, ResetsAt: progress.ResetsAt, RemainingSeconds: progress.RemainingSeconds}
}

func newPulseTokenStatsDTO(stats *usagestats.UsageStats) *PulseTokenStatsDTO {
	if stats == nil {
		return nil
	}
	return &PulseTokenStatsDTO{
		RequestCount: stats.TotalRequests,
		InputTokens:  stats.TotalInputTokens,
		OutputTokens: stats.TotalOutputTokens,
		CacheTokens:  stats.TotalCacheTokens,
		TotalTokens:  stats.TotalTokens,
	}
}

func scaledPercent(snapshot *QuotaSnapshot) int {
	percent := int(math.Round(GuestScaledUsedFraction(snapshot) * 100))
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func windowStart(snapshot *QuotaSnapshot) time.Time {
	if snapshot != nil && snapshot.ResetsAt != nil {
		return snapshot.ResetsAt.Add(-weeklyQuotaWindowDuration)
	}
	return time.Now().Add(-weeklyQuotaWindowDuration)
}

func usageSource(usage *UsageInfo, refresh bool) string {
	if usage != nil && usage.Source != "" {
		return usage.Source
	}
	if refresh {
		return "active"
	}
	return "passive"
}

func usageUpdatedAt(usage *UsageInfo) *time.Time {
	if usage == nil {
		return nil
	}
	return usage.UpdatedAt
}

func usageFiveHour(usage *UsageInfo) *UsageProgress {
	if usage == nil {
		return nil
	}
	return usage.FiveHour
}

func usageSevenDay(usage *UsageInfo) *UsageProgress {
	if usage == nil {
		return nil
	}
	return usage.SevenDay
}
