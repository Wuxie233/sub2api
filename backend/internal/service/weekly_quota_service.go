package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var ErrWeeklyQuotaGuestExhausted = errors.New("weekly quota guest exhausted")

const (
	weeklyQuotaRequestTTLSeconds = 24 * 60 * 60
	weeklyQuotaLedgerTTLSeconds  = 8 * 24 * 60 * 60
)

type WeeklyQuotaAccountUsageReader interface {
	GetPassiveUsage(ctx context.Context, accountID int64) (*UsageInfo, error)
}

type ReserveRequest struct {
	AccountID        int64
	APIKeyID         int64
	RequestID        string
	EstimatedCostUSD float64
}

type ReserveDecision struct {
	ResetsAt         *time.Time
	Managed          bool
	Admitted         bool
	Reason           string
	Role             string
	AccountID        int64
	ResetEpoch       int64
	RequestID        string
	ReserveUSD       float64
	RemainingSeconds int
}

type QuotaSnapshot struct {
	ResetsAt           *time.Time
	AccountID          int64
	Enabled            bool
	EffectiveBudgetUSD float64
	GuestCapUSD        float64
	OwnerCapUSD        float64
	GuestSettledUSD    float64
	OwnerSettledUSD    float64
	GuestReservedUSD   float64
	OwnerReservedUSD   float64
	OwnerOverflowUSD   float64
	GuestDepletionUSD  float64
	U7d                float64
	RemainingSeconds   int
}

type WeeklyQuotaService struct {
	cfg          *config.Config
	ledger       WeeklyQuotaLedger
	usageRepo    UsageLogRepository
	accountUsage WeeklyQuotaAccountUsageReader
	now          func() time.Time

	mu            sync.Mutex
	rebuiltEpochs map[SnapshotKey]struct{}
}

func NewWeeklyQuotaService(cfg *config.Config, ledger WeeklyQuotaLedger, usageRepo UsageLogRepository, accountUsage *AccountUsageService) *WeeklyQuotaService {
	return newWeeklyQuotaServiceWithUsageReader(cfg, ledger, usageRepo, accountUsage)
}

func newWeeklyQuotaServiceWithUsageReader(cfg *config.Config, ledger WeeklyQuotaLedger, usageRepo UsageLogRepository, accountUsage WeeklyQuotaAccountUsageReader) *WeeklyQuotaService {
	return &WeeklyQuotaService{
		cfg:           cfg,
		ledger:        ledger,
		usageRepo:     usageRepo,
		accountUsage:  accountUsage,
		now:           time.Now,
		rebuiltEpochs: make(map[SnapshotKey]struct{}),
	}
}

func (s *WeeklyQuotaService) Reserve(ctx context.Context, request ReserveRequest) (ReserveDecision, error) {
	car, role, managed := s.resolveRole(request.AccountID, request.APIKeyID)
	if !managed {
		return ReserveDecision{Managed: false, Admitted: true, AccountID: request.AccountID, RequestID: request.RequestID}, nil
	}
	state, _, effectiveBudgetUSD, err := s.prepare(ctx, car)
	if err != nil {
		return ReserveDecision{}, err
	}
	guestCapUSD, ownerCapUSD := weeklyQuotaCaps(effectiveBudgetUSD, car)
	reserveUSD := request.EstimatedCostUSD
	if reserveUSD <= 0 {
		reserveUSD = car.DefaultReserveUSD
	}
	result, err := s.ledger.Reserve(ctx, ReserveInput{
		AccountID:        request.AccountID,
		ResetEpoch:       state.resetEpoch,
		Role:             role,
		RequestID:        request.RequestID,
		ReserveUSD:       reserveUSD,
		GuestCapUSD:      guestCapUSD,
		OwnerCapUSD:      ownerCapUSD,
		GuestStopHit:     state.u7d >= car.GuestStopUtilization,
		OwnerStopHit:     state.u7d >= car.OwnerStopUtilization,
		ReqTTLSeconds:    weeklyQuotaRequestTTLSeconds,
		LedgerTTLSeconds: weeklyQuotaLedgerTTLSeconds,
	})
	if err != nil {
		return ReserveDecision{}, fmt.Errorf("reserve weekly quota ledger: %w", err)
	}
	return ReserveDecision{
		Managed:          true,
		Admitted:         result.Admitted,
		Reason:           result.Reason,
		Role:             role,
		AccountID:        request.AccountID,
		ResetEpoch:       state.resetEpoch,
		RequestID:        request.RequestID,
		ReserveUSD:       reserveUSD,
		ResetsAt:         state.resetsAt,
		RemainingSeconds: state.remainingSeconds,
	}, nil
}

func (s *WeeklyQuotaService) Settle(ctx context.Context, decision ReserveDecision, actualCostUSD float64) error {
	if !decision.Managed || !decision.Admitted {
		return nil
	}
	if err := s.ledger.Settle(ctx, SettleInput{AccountID: decision.AccountID, ResetEpoch: decision.ResetEpoch, RequestID: decision.RequestID, ActualUSD: actualCostUSD}); err != nil {
		return fmt.Errorf("settle weekly quota ledger: %w", err)
	}
	return nil
}

func (s *WeeklyQuotaService) Release(ctx context.Context, decision ReserveDecision) error {
	if !decision.Managed || !decision.Admitted {
		return nil
	}
	if err := s.ledger.Release(ctx, ReleaseInput{AccountID: decision.AccountID, ResetEpoch: decision.ResetEpoch, RequestID: decision.RequestID}); err != nil {
		return fmt.Errorf("release weekly quota ledger: %w", err)
	}
	return nil
}

func (s *WeeklyQuotaService) Snapshot(ctx context.Context, accountID int64) (*QuotaSnapshot, error) {
	car, ok := s.resolveCar(accountID)
	if !ok {
		return &QuotaSnapshot{AccountID: accountID, Enabled: false}, nil
	}
	state, snapshot, effectiveBudgetUSD, err := s.prepare(ctx, car)
	if err != nil {
		return nil, err
	}
	guestCapUSD, ownerCapUSD := weeklyQuotaCaps(effectiveBudgetUSD, car)
	ownerOverflowUSD := weeklyQuotaOwnerOverflow(snapshot, ownerCapUSD)
	guestDepletionUSD := snapshot.GuestSettledUSD + snapshot.GuestReservedUSD + ownerOverflowUSD
	return &QuotaSnapshot{
		AccountID:          accountID,
		Enabled:            true,
		EffectiveBudgetUSD: effectiveBudgetUSD,
		GuestCapUSD:        guestCapUSD,
		OwnerCapUSD:        ownerCapUSD,
		GuestSettledUSD:    snapshot.GuestSettledUSD,
		OwnerSettledUSD:    snapshot.OwnerSettledUSD,
		GuestReservedUSD:   snapshot.GuestReservedUSD,
		OwnerReservedUSD:   snapshot.OwnerReservedUSD,
		OwnerOverflowUSD:   ownerOverflowUSD,
		GuestDepletionUSD:  guestDepletionUSD,
		U7d:                state.u7d,
		ResetsAt:           state.resetsAt,
		RemainingSeconds:   state.remainingSeconds,
	}, nil
}

func (d ReserveDecision) GuestExhausted() bool {
	return d.Managed && !d.Admitted && d.Role == WeeklyQuotaRoleGuest && (d.Reason == WeeklyQuotaReasonGuestPoolFull || d.Reason == WeeklyQuotaReasonGuestSafetyLine)
}

func WeeklyQuotaGuestExhaustionError(decision ReserveDecision) error {
	if decision.GuestExhausted() {
		return ErrWeeklyQuotaGuestExhausted
	}
	return nil
}

func (d ReserveDecision) OwnerSafetyLineHit() bool {
	return d.Managed && !d.Admitted && d.Role == WeeklyQuotaRoleOwner && d.Reason == WeeklyQuotaReasonOwnerSafetyLine
}

func GuestScaledUsedFraction(snapshot *QuotaSnapshot) float64 {
	if snapshot == nil || snapshot.GuestCapUSD <= 0 {
		return 0
	}
	return clampFloat(snapshot.GuestDepletionUSD/snapshot.GuestCapUSD, 0, 1)
}

func (s *WeeklyQuotaService) resolveRole(accountID, apiKeyID int64) (*config.WeeklyQuotaCarConfig, string, bool) {
	car, ok := s.resolveCar(accountID)
	if !ok {
		return nil, "", false
	}
	role, managed := s.cfg.ResolveKeyRole(accountID, apiKeyID)
	return car, role, managed
}

func (s *WeeklyQuotaService) resolveCar(accountID int64) (*config.WeeklyQuotaCarConfig, bool) {
	if s == nil || s.cfg == nil || !s.cfg.WeeklyQuota.Enabled {
		return nil, false
	}
	return s.cfg.CarByAccountID(accountID)
}
