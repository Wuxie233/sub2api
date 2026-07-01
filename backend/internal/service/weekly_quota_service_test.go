package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestWeeklyQuotaService_GuestPoolSplit(t *testing.T) {
	// Given
	ctx := context.Background()
	fixture := newWeeklyQuotaServiceFixture(t)
	fixture.ledger.snapshots[fixture.key()] = LedgerSnapshot{EffectiveBudgetUSD: 1000, GuestSettledUSD: 660}

	// When
	decision, err := fixture.service.Reserve(ctx, ReserveRequest{AccountID: 9, APIKeyID: 23, RequestID: "guest-pool", EstimatedCostUSD: 1})

	// Then
	require.NoError(t, err)
	require.True(t, decision.Managed)
	require.False(t, decision.Admitted)
	require.Equal(t, WeeklyQuotaRoleGuest, decision.Role)
	require.Equal(t, WeeklyQuotaReasonGuestPoolFull, decision.Reason)
	require.Equal(t, fixture.resetAt.Unix(), decision.ResetEpoch)
}

func TestWeeklyQuotaService_OwnerOverflowConsumesGuestPool(t *testing.T) {
	// Given
	ctx := context.Background()
	fixture := newWeeklyQuotaServiceFixture(t)
	fixture.ledger.snapshots[fixture.key()] = LedgerSnapshot{EffectiveBudgetUSD: 1000, OwnerSettledUSD: 400, GuestSettledUSD: 600}

	// When
	decision, err := fixture.service.Reserve(ctx, ReserveRequest{AccountID: 9, APIKeyID: 23, RequestID: "owner-overflow", EstimatedCostUSD: 1})

	// Then
	require.NoError(t, err)
	require.False(t, decision.Admitted)
	require.Equal(t, WeeklyQuotaReasonGuestPoolFull, decision.Reason)
}

func TestWeeklyQuotaService_GuestSafetyLine(t *testing.T) {
	// Given
	ctx := context.Background()
	fixture := newWeeklyQuotaServiceFixture(t)
	fixture.usage.SevenDay.Utilization = 97
	fixture.ledger.snapshots[fixture.key()] = LedgerSnapshot{EffectiveBudgetUSD: 1000}

	// When
	decision, err := fixture.service.Reserve(ctx, ReserveRequest{AccountID: 9, APIKeyID: 23, RequestID: "guest-safety", EstimatedCostUSD: 1})

	// Then
	require.NoError(t, err)
	require.False(t, decision.Admitted)
	require.Equal(t, WeeklyQuotaReasonGuestSafetyLine, decision.Reason)
}

func TestWeeklyQuotaService_OwnerSafetyLine(t *testing.T) {
	// Given
	ctx := context.Background()
	fixture := newWeeklyQuotaServiceFixture(t)
	fixture.usage.SevenDay.Utilization = 98.5
	fixture.ledger.snapshots[fixture.key()] = LedgerSnapshot{EffectiveBudgetUSD: 1000}

	// When
	decision, err := fixture.service.Reserve(ctx, ReserveRequest{AccountID: 9, APIKeyID: 1, RequestID: "owner-safety", EstimatedCostUSD: 1})

	// Then
	require.NoError(t, err)
	require.False(t, decision.Admitted)
	require.Equal(t, WeeklyQuotaReasonOwnerSafetyLine, decision.Reason)
}

func TestWeeklyQuotaService_CalibrationIgnoresLowOrStale(t *testing.T) {
	// Given
	ctx := context.Background()
	fixture := newWeeklyQuotaServiceFixture(t)
	fixture.usageRepo.accountActualCost[9] = 60
	fixture.usage.SevenDay.Utilization = 3

	// When
	lowUtilSnap, err := fixture.service.Snapshot(ctx, 9)

	// Then
	require.NoError(t, err)
	require.InDelta(t, 1000, lowUtilSnap.EffectiveBudgetUSD, 0.000001)
	require.Zero(t, fixture.ledger.calibrationCalls)
	require.Zero(t, fixture.usageRepo.sumAccountCalls)

	// Given
	stale := newWeeklyQuotaServiceFixture(t)
	previous := 1800.0
	stale.usageRepo.accountActualCost[9] = 600
	stale.usage.UpdatedAt = timePtr(stale.now.Add(-time.Hour))
	stale.ledger.snapshots[stale.key()] = LedgerSnapshot{EffectiveBudgetUSD: previous}

	// When
	staleSnap, err := stale.service.Snapshot(ctx, 9)

	// Then
	require.NoError(t, err)
	require.InDelta(t, previous, staleSnap.EffectiveBudgetUSD, 0.000001)
	require.Zero(t, stale.ledger.calibrationCalls)
	require.Zero(t, stale.usageRepo.sumAccountCalls)
}

func TestWeeklyQuotaService_CalibrationDownFastUpSlow(t *testing.T) {
	// Given
	ctx := context.Background()
	down := newWeeklyQuotaServiceFixture(t)
	down.usage.SevenDay.Utilization = 50
	down.usageRepo.accountActualCost[9] = 500
	down.ledger.snapshots[down.key()] = LedgerSnapshot{EffectiveBudgetUSD: 2000}

	// When
	downSnap, err := down.service.Snapshot(ctx, 9)

	// Then
	require.NoError(t, err)
	require.InDelta(t, 1500, downSnap.EffectiveBudgetUSD, 0.000001)
	require.InDelta(t, 1000, down.ledger.snapshots[down.key()].LastObservedBudgetUSD, 0.000001)
	require.Equal(t, 1, down.ledger.calibrationCalls)

	// Given
	up := newWeeklyQuotaServiceFixture(t)
	up.usage.SevenDay.Utilization = 50
	up.usageRepo.accountActualCost[9] = 1500
	up.ledger.snapshots[up.key()] = LedgerSnapshot{EffectiveBudgetUSD: 2000}

	// When
	upSnap, err := up.service.Snapshot(ctx, 9)

	// Then
	require.NoError(t, err)
	require.InDelta(t, 2100, upSnap.EffectiveBudgetUSD, 0.000001)
	require.InDelta(t, 3000, up.ledger.snapshots[up.key()].LastObservedBudgetUSD, 0.000001)
	require.Equal(t, 1, up.ledger.calibrationCalls)
}

func TestWeeklyQuotaService_ResetRollover(t *testing.T) {
	// Given
	ctx := context.Background()
	fixture := newWeeklyQuotaServiceFixture(t)
	oldKey := fixture.key()
	fixture.ledger.snapshots[oldKey] = LedgerSnapshot{EffectiveBudgetUSD: 1000, GuestSettledUSD: 660}
	fixture.resetAt = fixture.resetAt.Add(7 * 24 * time.Hour)
	fixture.usage.SevenDay.ResetsAt = &fixture.resetAt

	// When
	decision, err := fixture.service.Reserve(ctx, ReserveRequest{AccountID: 9, APIKeyID: 23, RequestID: "rollover", EstimatedCostUSD: 1})

	// Then
	require.NoError(t, err)
	require.True(t, decision.Admitted)
	require.Equal(t, WeeklyQuotaReasonOK, decision.Reason)
	require.Equal(t, fixture.resetAt.Unix(), decision.ResetEpoch)
	newSnapshot := fixture.ledger.snapshots[fixture.key()]
	require.Zero(t, newSnapshot.GuestSettledUSD)
	require.InDelta(t, 1, newSnapshot.GuestReservedUSD, 0.000001)
}

func TestWeeklyQuotaService_ColdRebuildFromUsageLogs(t *testing.T) {
	// Given
	ctx := context.Background()
	fixture := newWeeklyQuotaServiceFixture(t)
	fixture.usageRepo.costByAPIKeyID[1] = 300
	fixture.usageRepo.costByAPIKeyID[3] = 40
	fixture.usageRepo.costByAPIKeyID[23] = 500
	fixture.usageRepo.costByAPIKeyID[24] = 100

	// When
	snap, err := fixture.service.Snapshot(ctx, 9)

	// Then
	require.NoError(t, err)
	require.Equal(t, 1, fixture.ledger.rebuildCalls)
	require.InDelta(t, 340, snap.OwnerSettledUSD, 0.000001)
	require.InDelta(t, 600, snap.GuestSettledUSD, 0.000001)
	require.Equal(t, fixture.resetAt.Add(-7*24*time.Hour), fixture.usageRepo.lastStartTime)
	require.Equal(t, fixture.now, fixture.usageRepo.lastEndTime)

	// When
	_, err = fixture.service.Reserve(ctx, ReserveRequest{AccountID: 9, APIKeyID: 23, RequestID: "after-rebuild", EstimatedCostUSD: 1})

	// Then
	require.NoError(t, err)
	require.Equal(t, 1, fixture.ledger.rebuildCalls)
}

func TestWeeklyQuotaService_UnmanagedKeyBypass(t *testing.T) {
	// Given
	ctx := context.Background()
	fixture := newWeeklyQuotaServiceFixture(t)

	// When
	decision, err := fixture.service.Reserve(ctx, ReserveRequest{AccountID: 9, APIKeyID: 99, RequestID: "unmanaged", EstimatedCostUSD: 1})

	// Then
	require.NoError(t, err)
	require.False(t, decision.Managed)
	require.True(t, decision.Admitted)
	require.Zero(t, fixture.ledger.reserveCalls)
	require.Zero(t, fixture.ledger.rebuildCalls)
}

type weeklyQuotaServiceFixture struct {
	service   *WeeklyQuotaService
	ledger    *weeklyQuotaLedgerFake
	usageRepo *weeklyQuotaUsageRepoFake
	usage     *UsageInfo
	now       time.Time
	resetAt   time.Time
}

func newWeeklyQuotaServiceFixture(t *testing.T) *weeklyQuotaServiceFixture {
	t.Helper()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	resetAt := now.Add(72 * time.Hour)
	usage := &UsageInfo{
		UpdatedAt: timePtr(now.Add(-time.Minute)),
		SevenDay: &UsageProgress{
			Utilization:      48,
			ResetsAt:         &resetAt,
			RemainingSeconds: int(resetAt.Sub(now).Seconds()),
		},
	}
	usageRepo := &weeklyQuotaUsageRepoFake{
		costByAPIKeyID:    make(map[int64]float64),
		accountActualCost: make(map[int64]float64),
	}
	ledger := newWeeklyQuotaLedgerFake()
	cfg := &config.Config{WeeklyQuota: config.WeeklyQuotaConfig{
		Enabled: true,
		Cars: []config.WeeklyQuotaCarConfig{{
			AccountID:                  9,
			BudgetSeedUSD:              1000,
			GuestShareBps:              6600,
			OwnerShareBps:              3400,
			MinCalibrationUtilization:  0.08,
			PassiveSampleMaxAgeSeconds: 900,
			GuestStopUtilization:       0.97,
			OwnerStopUtilization:       0.985,
			DefaultReserveUSD:          0.5,
			OwnerKeyIDs:                []int64{1, 3},
			GuestKeyIDs:                []int64{23, 24},
		}},
	}}
	accountUsage := &weeklyQuotaAccountUsageFake{usageByAccount: map[int64]*UsageInfo{9: usage}}
	service := newWeeklyQuotaServiceWithUsageReader(cfg, ledger, usageRepo, accountUsage)
	service.now = func() time.Time { return now }
	return &weeklyQuotaServiceFixture{
		service:   service,
		ledger:    ledger,
		usageRepo: usageRepo,
		usage:     usage,
		now:       now,
		resetAt:   resetAt,
	}
}

func (f *weeklyQuotaServiceFixture) key() SnapshotKey {
	return SnapshotKey{AccountID: 9, ResetEpoch: f.resetAt.Unix()}
}

func timePtr(value time.Time) *time.Time {
	return &value
}
