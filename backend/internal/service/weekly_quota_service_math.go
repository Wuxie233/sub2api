package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	weeklyQuotaWindowDuration = 7 * 24 * time.Hour
	weeklyQuotaAlphaDown      = 0.5
	weeklyQuotaAlphaUp        = 0.1
	weeklyQuotaFloorRatio     = 0.25
)

type weeklyQuotaWindowState struct {
	resetsAt         *time.Time
	windowStart      time.Time
	windowEnd        time.Time
	resetEpoch       int64
	remainingSeconds int
	u7d              float64
	calibratable     bool
}

func (s *WeeklyQuotaService) prepare(ctx context.Context, car *config.WeeklyQuotaCarConfig) (weeklyQuotaWindowState, LedgerSnapshot, float64, error) {
	state, err := s.windowState(ctx, car)
	if err != nil {
		return weeklyQuotaWindowState{}, LedgerSnapshot{}, 0, err
	}
	key := SnapshotKey{AccountID: car.AccountID, ResetEpoch: state.resetEpoch}
	snapshot, err := s.ledger.Snapshot(ctx, key)
	if err != nil {
		return weeklyQuotaWindowState{}, LedgerSnapshot{}, 0, fmt.Errorf("snapshot weekly quota ledger: %w", err)
	}
	if s.needsColdRebuild(key, snapshot) {
		snapshot, err = s.rebuild(ctx, car, state, key)
		if err != nil {
			return weeklyQuotaWindowState{}, LedgerSnapshot{}, 0, err
		}
	}
	effectiveBudgetUSD, err := s.effectiveBudget(ctx, car, state, snapshot)
	if err != nil {
		return weeklyQuotaWindowState{}, LedgerSnapshot{}, 0, err
	}
	snapshot.EffectiveBudgetUSD = effectiveBudgetUSD
	return state, snapshot, effectiveBudgetUSD, nil
}

func (s *WeeklyQuotaService) windowState(ctx context.Context, car *config.WeeklyQuotaCarConfig) (weeklyQuotaWindowState, error) {
	usage, err := s.accountUsage.GetPassiveUsage(ctx, car.AccountID)
	if err != nil {
		return weeklyQuotaWindowState{}, fmt.Errorf("get passive weekly quota usage: %w", err)
	}
	now := s.now()
	state := weeklyQuotaWindowState{
		resetEpoch:  fallbackWeeklyQuotaResetEpoch(now),
		windowStart: now.Add(-weeklyQuotaWindowDuration),
		windowEnd:   now,
	}
	if usage != nil && usage.SevenDay != nil {
		state.u7d = clampFloat(usage.SevenDay.Utilization/100, 0, 1)
		state.remainingSeconds = usage.SevenDay.RemainingSeconds
		if usage.SevenDay.ResetsAt != nil {
			resetCopy := *usage.SevenDay.ResetsAt
			state.resetsAt = &resetCopy
			state.resetEpoch = resetCopy.Unix()
			state.windowStart = resetCopy.Add(-weeklyQuotaWindowDuration)
			state.windowEnd = now
			if state.remainingSeconds <= 0 {
				state.remainingSeconds = int(resetCopy.Sub(now).Seconds())
				if state.remainingSeconds < 0 {
					state.remainingSeconds = 0
				}
			}
		}
	}
	state.calibratable = usageSampleFresh(usage, now, car.PassiveSampleMaxAgeSeconds) && state.resetsAt != nil && state.u7d >= car.MinCalibrationUtilization
	return state, nil
}

func (s *WeeklyQuotaService) rebuild(ctx context.Context, car *config.WeeklyQuotaCarConfig, state weeklyQuotaWindowState, key SnapshotKey) (LedgerSnapshot, error) {
	ownerSettledUSD, err := s.usageRepo.SumActualCostByAPIKeyIDs(ctx, car.AccountID, car.OwnerKeyIDs, state.windowStart, state.windowEnd)
	if err != nil {
		return LedgerSnapshot{}, fmt.Errorf("sum owner weekly quota cost: %w", err)
	}
	guestSettledUSD, err := s.usageRepo.SumActualCostByAPIKeyIDs(ctx, car.AccountID, car.GuestKeyIDs, state.windowStart, state.windowEnd)
	if err != nil {
		return LedgerSnapshot{}, fmt.Errorf("sum guest weekly quota cost: %w", err)
	}
	if err := s.ledger.Rebuild(ctx, RebuildInput{AccountID: car.AccountID, ResetEpoch: state.resetEpoch, OwnerSettledUSD: ownerSettledUSD, GuestSettledUSD: guestSettledUSD, LedgerTTLSeconds: weeklyQuotaLedgerTTLSeconds}); err != nil {
		return LedgerSnapshot{}, fmt.Errorf("rebuild weekly quota ledger: %w", err)
	}
	s.markRebuilt(key)
	snapshot, err := s.ledger.Snapshot(ctx, key)
	if err != nil {
		return LedgerSnapshot{}, fmt.Errorf("snapshot rebuilt weekly quota ledger: %w", err)
	}
	return snapshot, nil
}

func (s *WeeklyQuotaService) effectiveBudget(ctx context.Context, car *config.WeeklyQuotaCarConfig, state weeklyQuotaWindowState, snapshot LedgerSnapshot) (float64, error) {
	effectiveBudgetUSD := snapshot.EffectiveBudgetUSD
	if state.calibratable {
		accountCostUSD, err := s.usageRepo.SumAccountActualCost(ctx, car.AccountID, state.windowStart, state.windowEnd)
		if err != nil {
			return 0, fmt.Errorf("sum account weekly quota cost: %w", err)
		}
		observedBudgetUSD := accountCostUSD / state.u7d
		effectiveBudgetUSD = ewmaWeeklyQuotaBudget(snapshot.EffectiveBudgetUSD, observedBudgetUSD)
		effectiveBudgetUSD = applyWeeklyQuotaBudgetFloor(effectiveBudgetUSD, car.BudgetSeedUSD)
		if err := s.ledger.SetCalibration(ctx, CalibrationInput{AccountID: car.AccountID, ResetEpoch: state.resetEpoch, EffectiveBudgetUSD: effectiveBudgetUSD, LastObservedBudgetUSD: observedBudgetUSD, LastCalibratedUnix: s.now().Unix(), LedgerTTLSeconds: weeklyQuotaLedgerTTLSeconds}); err != nil {
			return 0, fmt.Errorf("set weekly quota calibration: %w", err)
		}
		return effectiveBudgetUSD, nil
	}
	if effectiveBudgetUSD <= 0 {
		effectiveBudgetUSD = car.BudgetSeedUSD
	}
	return applyWeeklyQuotaBudgetFloor(effectiveBudgetUSD, car.BudgetSeedUSD), nil
}

func (s *WeeklyQuotaService) needsColdRebuild(key SnapshotKey, snapshot LedgerSnapshot) bool {
	if !ledgerSnapshotEmpty(snapshot) || s.alreadyRebuilt(key) {
		return false
	}
	return true
}

func (s *WeeklyQuotaService) alreadyRebuilt(key SnapshotKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.rebuiltEpochs[key]
	return ok
}

func (s *WeeklyQuotaService) markRebuilt(key SnapshotKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rebuiltEpochs[key] = struct{}{}
}

func ledgerSnapshotEmpty(snapshot LedgerSnapshot) bool {
	return snapshot.OwnerSettledUSD == 0 && snapshot.GuestSettledUSD == 0 && snapshot.OwnerReservedUSD == 0 && snapshot.GuestReservedUSD == 0 && snapshot.EffectiveBudgetUSD == 0 && snapshot.LastObservedBudgetUSD == 0 && snapshot.LastCalibratedUnix == 0
}

func ewmaWeeklyQuotaBudget(previousBudgetUSD, observedBudgetUSD float64) float64 {
	if previousBudgetUSD <= 0 {
		return observedBudgetUSD
	}
	alpha := weeklyQuotaAlphaUp
	if observedBudgetUSD < previousBudgetUSD {
		alpha = weeklyQuotaAlphaDown
	}
	return previousBudgetUSD + alpha*(observedBudgetUSD-previousBudgetUSD)
}

func applyWeeklyQuotaBudgetFloor(effectiveBudgetUSD, seedBudgetUSD float64) float64 {
	floor := seedBudgetUSD * weeklyQuotaFloorRatio
	if effectiveBudgetUSD < floor {
		return floor
	}
	return effectiveBudgetUSD
}

func weeklyQuotaCaps(effectiveBudgetUSD float64, car *config.WeeklyQuotaCarConfig) (float64, float64) {
	guestCapUSD := effectiveBudgetUSD * float64(car.GuestShareBps) / 10000
	ownerCapUSD := effectiveBudgetUSD * float64(car.OwnerShareBps) / 10000
	return guestCapUSD, ownerCapUSD
}

func weeklyQuotaOwnerOverflow(snapshot LedgerSnapshot, ownerCapUSD float64) float64 {
	ownerUsedUSD := snapshot.OwnerSettledUSD + snapshot.OwnerReservedUSD
	if ownerUsedUSD <= ownerCapUSD {
		return 0
	}
	return ownerUsedUSD - ownerCapUSD
}

func usageSampleFresh(usage *UsageInfo, now time.Time, maxAgeSeconds int) bool {
	if usage == nil || usage.UpdatedAt == nil || maxAgeSeconds <= 0 {
		return false
	}
	age := now.Sub(*usage.UpdatedAt)
	return age >= 0 && age <= time.Duration(maxAgeSeconds)*time.Second
}

func fallbackWeeklyQuotaResetEpoch(now time.Time) int64 {
	return now.UTC().Truncate(weeklyQuotaWindowDuration).Add(weeklyQuotaWindowDuration).Unix()
}

func clampFloat(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
