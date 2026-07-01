package service

import "context"

const (
	WeeklyQuotaRoleOwner = "owner"
	WeeklyQuotaRoleGuest = "guest"

	WeeklyQuotaReasonOK              = "ok"
	WeeklyQuotaReasonGuestPoolFull   = "guest_pool_full"
	WeeklyQuotaReasonGuestSafetyLine = "guest_safety_line"
	WeeklyQuotaReasonOwnerSafetyLine = "owner_safety_line"
	WeeklyQuotaReasonAlreadyReserved = "already_reserved"
)

type WeeklyQuotaLedger interface {
	Reserve(ctx context.Context, input ReserveInput) (ReserveResult, error)
	Settle(ctx context.Context, input SettleInput) error
	Release(ctx context.Context, input ReleaseInput) error
	Rebuild(ctx context.Context, input RebuildInput) error
	Snapshot(ctx context.Context, key SnapshotKey) (LedgerSnapshot, error)
	SetCalibration(ctx context.Context, input CalibrationInput) error
}

type ReserveInput struct {
	AccountID        int64
	ResetEpoch       int64
	Role             string
	RequestID        string
	ReserveUSD       float64
	GuestCapUSD      float64
	OwnerCapUSD      float64
	GuestStopHit     bool
	OwnerStopHit     bool
	ReqTTLSeconds    int
	LedgerTTLSeconds int
}

type ReserveResult struct {
	Admitted          bool
	Reason            string
	GuestDepletionUSD float64
	GuestCapUSD       float64
}

type SettleInput struct {
	AccountID  int64
	ResetEpoch int64
	RequestID  string
	ActualUSD  float64
}

type ReleaseInput struct {
	AccountID  int64
	ResetEpoch int64
	RequestID  string
}

type RebuildInput struct {
	AccountID        int64
	ResetEpoch       int64
	OwnerSettledUSD  float64
	GuestSettledUSD  float64
	LedgerTTLSeconds int
}

type SnapshotKey struct {
	AccountID  int64
	ResetEpoch int64
}

type LedgerSnapshot struct {
	OwnerSettledUSD       float64
	GuestSettledUSD       float64
	OwnerReservedUSD      float64
	GuestReservedUSD      float64
	EffectiveBudgetUSD    float64
	LastObservedBudgetUSD float64
	LastCalibratedUnix    int64
}

type CalibrationInput struct {
	AccountID             int64
	ResetEpoch            int64
	EffectiveBudgetUSD    float64
	LastObservedBudgetUSD float64
	LastCalibratedUnix    int64
	LedgerTTLSeconds      int
}
