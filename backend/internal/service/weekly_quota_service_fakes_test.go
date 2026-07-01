package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type weeklyQuotaAccountRepoFake struct {
	AccountRepository

	accounts map[int64]*Account
}

func (r *weeklyQuotaAccountRepoFake) GetByID(ctx context.Context, id int64) (*Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	account, ok := r.accounts[id]
	if !ok {
		return nil, ErrAccountNotFound
	}
	return account, nil
}

type weeklyQuotaUsageRepoFake struct {
	UsageLogRepository

	costByAPIKeyID    map[int64]float64
	accountActualCost map[int64]float64
	sumByKeysCalls    int
	sumAccountCalls   int
	lastStartTime     time.Time
	lastEndTime       time.Time
}

type weeklyQuotaAccountUsageFake struct {
	usageByAccount map[int64]*UsageInfo
}

func (f *weeklyQuotaAccountUsageFake) GetPassiveUsage(ctx context.Context, accountID int64) (*UsageInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.usageByAccount[accountID], nil
}

func (r *weeklyQuotaUsageRepoFake) SumActualCostByAPIKeyIDs(ctx context.Context, _ int64, apiKeyIDs []int64, startTime, endTime time.Time) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.sumByKeysCalls++
	r.lastStartTime = startTime
	r.lastEndTime = endTime
	var total float64
	for _, apiKeyID := range apiKeyIDs {
		total += r.costByAPIKeyID[apiKeyID]
	}
	return total, nil
}

func (r *weeklyQuotaUsageRepoFake) SumAccountActualCost(ctx context.Context, accountID int64, startTime, endTime time.Time) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.sumAccountCalls++
	r.lastStartTime = startTime
	r.lastEndTime = endTime
	return r.accountActualCost[accountID], nil
}

func (r *weeklyQuotaUsageRepoFake) GuestOwnWindowStats(ctx context.Context, _ int64, _, _ time.Time) (GuestOwnStats, error) {
	if err := ctx.Err(); err != nil {
		return GuestOwnStats{}, err
	}
	return GuestOwnStats{}, nil
}

func (r *weeklyQuotaUsageRepoFake) GetAccountWindowStats(ctx context.Context, _ int64, _ time.Time) (*usagestats.AccountStats, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &usagestats.AccountStats{}, nil
}

type weeklyQuotaLedgerFake struct {
	snapshots        map[SnapshotKey]LedgerSnapshot
	reservations     map[string]weeklyQuotaReservationFake
	reserveCalls     int
	settleCalls      int
	releaseCalls     int
	rebuildCalls     int
	calibrationCalls int
}

type weeklyQuotaReservationFake struct {
	key    SnapshotKey
	role   string
	amount float64
	state  string
}

func newWeeklyQuotaLedgerFake() *weeklyQuotaLedgerFake {
	return &weeklyQuotaLedgerFake{
		snapshots:    make(map[SnapshotKey]LedgerSnapshot),
		reservations: make(map[string]weeklyQuotaReservationFake),
	}
}

func (l *weeklyQuotaLedgerFake) Reserve(ctx context.Context, input ReserveInput) (ReserveResult, error) {
	if err := ctx.Err(); err != nil {
		return ReserveResult{}, err
	}
	l.reserveCalls++
	key := SnapshotKey{AccountID: input.AccountID, ResetEpoch: input.ResetEpoch}
	snapshot := l.snapshots[key]
	guestDepletion := weeklyQuotaGuestDepletion(snapshot, input.OwnerCapUSD)
	if existing, ok := l.reservations[input.RequestID]; ok && (existing.state == "reserved" || existing.state == "settled") {
		return ReserveResult{Admitted: true, Reason: WeeklyQuotaReasonAlreadyReserved, GuestDepletionUSD: guestDepletion, GuestCapUSD: input.GuestCapUSD}, nil
	}

	switch input.Role {
	case WeeklyQuotaRoleGuest:
		if input.GuestStopHit {
			return ReserveResult{Reason: WeeklyQuotaReasonGuestSafetyLine, GuestDepletionUSD: guestDepletion, GuestCapUSD: input.GuestCapUSD}, nil
		}
		if guestDepletion+input.ReserveUSD > input.GuestCapUSD {
			return ReserveResult{Reason: WeeklyQuotaReasonGuestPoolFull, GuestDepletionUSD: guestDepletion, GuestCapUSD: input.GuestCapUSD}, nil
		}
		snapshot.GuestReservedUSD += input.ReserveUSD
	case WeeklyQuotaRoleOwner:
		if input.OwnerStopHit {
			return ReserveResult{Reason: WeeklyQuotaReasonOwnerSafetyLine, GuestDepletionUSD: guestDepletion, GuestCapUSD: input.GuestCapUSD}, nil
		}
		snapshot.OwnerReservedUSD += input.ReserveUSD
	default:
		return ReserveResult{}, errors.New("invalid weekly quota role")
	}
	l.snapshots[key] = snapshot
	l.reservations[input.RequestID] = weeklyQuotaReservationFake{key: key, role: input.Role, amount: input.ReserveUSD, state: "reserved"}
	return ReserveResult{Admitted: true, Reason: WeeklyQuotaReasonOK, GuestDepletionUSD: guestDepletion, GuestCapUSD: input.GuestCapUSD}, nil
}

func (l *weeklyQuotaLedgerFake) Settle(ctx context.Context, input SettleInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.settleCalls++
	reservation, ok := l.reservations[input.RequestID]
	if !ok || reservation.state != "reserved" {
		return nil
	}
	snapshot := l.snapshots[reservation.key]
	if reservation.role == WeeklyQuotaRoleGuest {
		snapshot.GuestReservedUSD -= reservation.amount
		snapshot.GuestSettledUSD += input.ActualUSD
	} else {
		snapshot.OwnerReservedUSD -= reservation.amount
		snapshot.OwnerSettledUSD += input.ActualUSD
	}
	l.snapshots[reservation.key] = snapshot
	reservation.state = "settled"
	l.reservations[input.RequestID] = reservation
	return nil
}

func (l *weeklyQuotaLedgerFake) Release(ctx context.Context, input ReleaseInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.releaseCalls++
	reservation, ok := l.reservations[input.RequestID]
	if !ok || reservation.state != "reserved" {
		return nil
	}
	snapshot := l.snapshots[reservation.key]
	if reservation.role == WeeklyQuotaRoleGuest {
		snapshot.GuestReservedUSD -= reservation.amount
	} else {
		snapshot.OwnerReservedUSD -= reservation.amount
	}
	l.snapshots[reservation.key] = snapshot
	reservation.state = "released"
	l.reservations[input.RequestID] = reservation
	return nil
}

func (l *weeklyQuotaLedgerFake) Rebuild(ctx context.Context, input RebuildInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.rebuildCalls++
	key := SnapshotKey{AccountID: input.AccountID, ResetEpoch: input.ResetEpoch}
	snapshot := l.snapshots[key]
	snapshot.OwnerSettledUSD = input.OwnerSettledUSD
	snapshot.GuestSettledUSD = input.GuestSettledUSD
	l.snapshots[key] = snapshot
	return nil
}

func (l *weeklyQuotaLedgerFake) Snapshot(ctx context.Context, key SnapshotKey) (LedgerSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return LedgerSnapshot{}, err
	}
	return l.snapshots[key], nil
}

func (l *weeklyQuotaLedgerFake) SetCalibration(ctx context.Context, input CalibrationInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.calibrationCalls++
	key := SnapshotKey{AccountID: input.AccountID, ResetEpoch: input.ResetEpoch}
	snapshot := l.snapshots[key]
	snapshot.EffectiveBudgetUSD = input.EffectiveBudgetUSD
	snapshot.LastObservedBudgetUSD = input.LastObservedBudgetUSD
	snapshot.LastCalibratedUnix = input.LastCalibratedUnix
	l.snapshots[key] = snapshot
	return nil
}

func weeklyQuotaGuestDepletion(snapshot LedgerSnapshot, ownerCapUSD float64) float64 {
	ownerUsed := snapshot.OwnerSettledUSD + snapshot.OwnerReservedUSD
	ownerOverflow := ownerUsed - ownerCapUSD
	if ownerOverflow < 0 {
		ownerOverflow = 0
	}
	return snapshot.GuestSettledUSD + snapshot.GuestReservedUSD + ownerOverflow
}
