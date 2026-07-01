package repository

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func newWeeklyQuotaLedgerTestRepo(t *testing.T) service.WeeklyQuotaLedger {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	return NewWeeklyQuotaLedger(rdb)
}

func reserveInput(requestID string) service.ReserveInput {
	return service.ReserveInput{
		AccountID:        9,
		ResetEpoch:       1_800_000_000,
		Role:             service.WeeklyQuotaRoleGuest,
		RequestID:        requestID,
		ReserveUSD:       2,
		GuestCapUSD:      10,
		OwnerCapUSD:      100,
		ReqTTLSeconds:    300,
		LedgerTTLSeconds: 600,
	}
}

func TestWeeklyQuotaLedger_ReserveGuestConcurrentOnlyOneAdmitted(t *testing.T) {
	// Given
	ctx := context.Background()
	ledger := newWeeklyQuotaLedgerTestRepo(t)
	rebuild := service.RebuildInput{
		AccountID:        9,
		ResetEpoch:       1_800_000_000,
		GuestSettledUSD:  8,
		LedgerTTLSeconds: 600,
	}
	require.NoError(t, ledger.Rebuild(ctx, rebuild))

	inputs := []service.ReserveInput{reserveInput("weekly-concurrent-a"), reserveInput("weekly-concurrent-b")}
	results := make([]service.ReserveResult, len(inputs))
	errs := make([]error, len(inputs))
	var wg sync.WaitGroup

	// When
	for i := range inputs {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = ledger.Reserve(ctx, inputs[index])
		}(i)
	}
	wg.Wait()

	// Then
	admitted := 0
	rejectedFull := 0
	for i := range results {
		require.NoError(t, errs[i])
		if results[i].Admitted {
			admitted++
			continue
		}
		require.Equal(t, service.WeeklyQuotaReasonGuestPoolFull, results[i].Reason)
		rejectedFull++
	}
	require.Equal(t, 1, admitted)
	require.Equal(t, 1, rejectedFull)
}

func TestWeeklyQuotaLedger_ReserveIdempotentByRequestID(t *testing.T) {
	// Given
	ctx := context.Background()
	ledger := newWeeklyQuotaLedgerTestRepo(t)
	input := reserveInput("weekly-idempotent-reserve")

	// When
	first, err := ledger.Reserve(ctx, input)
	require.NoError(t, err)
	second, err := ledger.Reserve(ctx, input)
	require.NoError(t, err)
	snapshot, err := ledger.Snapshot(ctx, service.SnapshotKey{AccountID: input.AccountID, ResetEpoch: input.ResetEpoch})

	// Then
	require.NoError(t, err)
	require.True(t, first.Admitted)
	require.Equal(t, service.WeeklyQuotaReasonOK, first.Reason)
	require.True(t, second.Admitted)
	require.Equal(t, service.WeeklyQuotaReasonAlreadyReserved, second.Reason)
	require.InDelta(t, input.ReserveUSD, snapshot.GuestReservedUSD, 0.000001)
}

func TestWeeklyQuotaLedger_SettleIdempotent(t *testing.T) {
	// Given
	ctx := context.Background()
	ledger := newWeeklyQuotaLedgerTestRepo(t)
	input := reserveInput("weekly-idempotent-settle")
	require.NoError(t, mustReserve(ctx, ledger, input))
	settle := service.SettleInput{AccountID: input.AccountID, ResetEpoch: input.ResetEpoch, RequestID: input.RequestID, ActualUSD: 1.25}

	// When
	require.NoError(t, ledger.Settle(ctx, settle))
	require.NoError(t, ledger.Settle(ctx, settle))
	snapshot, err := ledger.Snapshot(ctx, service.SnapshotKey{AccountID: input.AccountID, ResetEpoch: input.ResetEpoch})

	// Then
	require.NoError(t, err)
	require.InDelta(t, 1.25, snapshot.GuestSettledUSD, 0.000001)
	require.Zero(t, snapshot.GuestReservedUSD)
}

func TestWeeklyQuotaLedger_ReleaseAfterSettleNoop(t *testing.T) {
	// Given
	ctx := context.Background()
	ledger := newWeeklyQuotaLedgerTestRepo(t)
	input := reserveInput("weekly-release-after-settle")
	require.NoError(t, mustReserve(ctx, ledger, input))
	settle := service.SettleInput{AccountID: input.AccountID, ResetEpoch: input.ResetEpoch, RequestID: input.RequestID, ActualUSD: 1.5}
	require.NoError(t, ledger.Settle(ctx, settle))

	// When
	require.NoError(t, ledger.Release(ctx, service.ReleaseInput{AccountID: input.AccountID, ResetEpoch: input.ResetEpoch, RequestID: input.RequestID}))
	snapshot, err := ledger.Snapshot(ctx, service.SnapshotKey{AccountID: input.AccountID, ResetEpoch: input.ResetEpoch})

	// Then
	require.NoError(t, err)
	require.InDelta(t, 1.5, snapshot.GuestSettledUSD, 0.000001)
	require.Zero(t, snapshot.GuestReservedUSD)
}

func TestWeeklyQuotaLedger_RebuildPreservesReservations(t *testing.T) {
	// Given
	ctx := context.Background()
	ledger := newWeeklyQuotaLedgerTestRepo(t)
	input := reserveInput("weekly-rebuild-preserve")
	require.NoError(t, mustReserve(ctx, ledger, input))

	// When
	require.NoError(t, ledger.Rebuild(ctx, service.RebuildInput{
		AccountID:        input.AccountID,
		ResetEpoch:       input.ResetEpoch,
		OwnerSettledUSD:  3,
		GuestSettledUSD:  4,
		LedgerTTLSeconds: 600,
	}))
	snapshot, err := ledger.Snapshot(ctx, service.SnapshotKey{AccountID: input.AccountID, ResetEpoch: input.ResetEpoch})

	// Then
	require.NoError(t, err)
	require.InDelta(t, 3, snapshot.OwnerSettledUSD, 0.000001)
	require.InDelta(t, 4, snapshot.GuestSettledUSD, 0.000001)
	require.InDelta(t, input.ReserveUSD, snapshot.GuestReservedUSD, 0.000001)
}

func TestWeeklyQuotaLedger_OwnerReserveNotPoolCapped(t *testing.T) {
	// Given
	ctx := context.Background()
	ledger := newWeeklyQuotaLedgerTestRepo(t)
	input := reserveInput("weekly-owner-reserve")
	input.Role = service.WeeklyQuotaRoleOwner
	input.OwnerCapUSD = 0.01
	input.GuestCapUSD = 0.01

	// When
	admitted, err := ledger.Reserve(ctx, input)
	require.NoError(t, err)
	input.RequestID = "weekly-owner-stop-hit"
	input.OwnerStopHit = true
	rejected, err := ledger.Reserve(ctx, input)
	snapshot, snapErr := ledger.Snapshot(ctx, service.SnapshotKey{AccountID: input.AccountID, ResetEpoch: input.ResetEpoch})

	// Then
	require.NoError(t, err)
	require.NoError(t, snapErr)
	require.True(t, admitted.Admitted)
	require.Equal(t, service.WeeklyQuotaReasonOK, admitted.Reason)
	require.False(t, rejected.Admitted)
	require.Equal(t, service.WeeklyQuotaReasonOwnerSafetyLine, rejected.Reason)
	require.InDelta(t, 2, snapshot.OwnerReservedUSD, 0.000001)
}

func mustReserve(ctx context.Context, ledger service.WeeklyQuotaLedger, input service.ReserveInput) error {
	result, err := ledger.Reserve(ctx, input)
	if err != nil {
		return err
	}
	if !result.Admitted {
		return errors.New("weekly quota reserve rejected")
	}
	return nil
}
