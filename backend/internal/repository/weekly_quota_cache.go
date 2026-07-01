package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	weeklyQuotaLedgerKeyFormat = "quota:weekly:%d:%d:ledger"
	weeklyQuotaReqKeyPrefix    = "quota:weekly:req:"
)

type weeklyQuotaLedger struct {
	rdb *redis.Client
}

func NewWeeklyQuotaLedger(rdb *redis.Client) service.WeeklyQuotaLedger {
	return &weeklyQuotaLedger{rdb: rdb}
}

func (l *weeklyQuotaLedger) Reserve(ctx context.Context, input service.ReserveInput) (service.ReserveResult, error) {
	result, err := weeklyQuotaReserveScript.Run(ctx, l.rdb, []string{ledgerKey(input.AccountID, input.ResetEpoch), requestKey(input.RequestID)},
		input.Role,
		formatUSD(input.ReserveUSD),
		formatUSD(input.OwnerCapUSD),
		formatUSD(input.GuestCapUSD),
		boolArg(input.GuestStopHit),
		boolArg(input.OwnerStopHit),
		input.ReqTTLSeconds,
		input.LedgerTTLSeconds,
		strconv.FormatInt(input.AccountID, 10),
		strconv.FormatInt(input.ResetEpoch, 10),
	).Result()
	if err != nil {
		return service.ReserveResult{}, fmt.Errorf("reserve weekly quota: %w", err)
	}
	return parseReserveResult(result)
}

func (l *weeklyQuotaLedger) Settle(ctx context.Context, input service.SettleInput) error {
	_, err := weeklyQuotaSettleScript.Run(ctx, l.rdb, []string{ledgerKey(input.AccountID, input.ResetEpoch), requestKey(input.RequestID)}, formatUSD(input.ActualUSD)).Result()
	if err != nil {
		return fmt.Errorf("settle weekly quota: %w", err)
	}
	return nil
}

func (l *weeklyQuotaLedger) Release(ctx context.Context, input service.ReleaseInput) error {
	_, err := weeklyQuotaReleaseScript.Run(ctx, l.rdb, []string{ledgerKey(input.AccountID, input.ResetEpoch), requestKey(input.RequestID)}).Result()
	if err != nil {
		return fmt.Errorf("release weekly quota: %w", err)
	}
	return nil
}

func (l *weeklyQuotaLedger) Rebuild(ctx context.Context, input service.RebuildInput) error {
	_, err := weeklyQuotaRebuildScript.Run(ctx, l.rdb, []string{ledgerKey(input.AccountID, input.ResetEpoch)},
		formatUSD(input.OwnerSettledUSD),
		formatUSD(input.GuestSettledUSD),
		input.LedgerTTLSeconds,
	).Result()
	if err != nil {
		return fmt.Errorf("rebuild weekly quota: %w", err)
	}
	return nil
}

func (l *weeklyQuotaLedger) Snapshot(ctx context.Context, key service.SnapshotKey) (service.LedgerSnapshot, error) {
	fields, err := l.rdb.HGetAll(ctx, ledgerKey(key.AccountID, key.ResetEpoch)).Result()
	if err != nil {
		return service.LedgerSnapshot{}, fmt.Errorf("snapshot weekly quota: %w", err)
	}
	return parseLedgerSnapshot(fields), nil
}

func (l *weeklyQuotaLedger) SetCalibration(ctx context.Context, input service.CalibrationInput) error {
	_, err := weeklyQuotaCalibrationScript.Run(ctx, l.rdb, []string{ledgerKey(input.AccountID, input.ResetEpoch)},
		formatUSD(input.EffectiveBudgetUSD),
		formatUSD(input.LastObservedBudgetUSD),
		strconv.FormatInt(input.LastCalibratedUnix, 10),
		input.LedgerTTLSeconds,
	).Result()
	if err != nil {
		return fmt.Errorf("set weekly quota calibration: %w", err)
	}
	return nil
}

func ledgerKey(accountID int64, resetEpoch int64) string {
	return fmt.Sprintf(weeklyQuotaLedgerKeyFormat, accountID, resetEpoch)
}

func requestKey(requestID string) string {
	return weeklyQuotaReqKeyPrefix + requestID
}

func boolArg(value bool) int {
	if value {
		return 1
	}
	return 0
}

func formatUSD(value float64) string {
	return strconv.FormatFloat(value, 'f', 10, 64)
}

func parseReserveResult(raw any) (service.ReserveResult, error) {
	values, ok := raw.([]any)
	if !ok || len(values) != 4 {
		return service.ReserveResult{}, fmt.Errorf("unexpected weekly quota reserve result: %T", raw)
	}
	admitted, err := parseScriptBool(values[0])
	if err != nil {
		return service.ReserveResult{}, err
	}
	guestDepletion, err := parseScriptFloat(values[2])
	if err != nil {
		return service.ReserveResult{}, err
	}
	guestCap, err := parseScriptFloat(values[3])
	if err != nil {
		return service.ReserveResult{}, err
	}
	return service.ReserveResult{
		Admitted:          admitted,
		Reason:            fmt.Sprint(values[1]),
		GuestDepletionUSD: guestDepletion,
		GuestCapUSD:       guestCap,
	}, nil
}

func parseScriptBool(value any) (bool, error) {
	n, err := parseScriptInt(value)
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func parseScriptInt(value any) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, fmt.Errorf("unexpected weekly quota integer result: %T", value)
	}
}

func parseScriptFloat(value any) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case int64:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("unexpected weekly quota float result: %T", value)
	}
}

func parseLedgerSnapshot(fields map[string]string) service.LedgerSnapshot {
	return service.LedgerSnapshot{
		OwnerSettledUSD:       parseSnapshotFloat(fields["owner_settled"]),
		GuestSettledUSD:       parseSnapshotFloat(fields["guest_settled"]),
		OwnerReservedUSD:      parseSnapshotFloat(fields["owner_reserved"]),
		GuestReservedUSD:      parseSnapshotFloat(fields["guest_reserved"]),
		EffectiveBudgetUSD:    parseSnapshotFloat(fields["effective_budget_usd"]),
		LastObservedBudgetUSD: parseSnapshotFloat(fields["last_observed_budget_usd"]),
		LastCalibratedUnix:    parseSnapshotInt(fields["last_calibrated_unix"]),
	}
}

func parseSnapshotFloat(value string) float64 {
	parsed, _ := strconv.ParseFloat(value, 64)
	return parsed
}

func parseSnapshotInt(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}
