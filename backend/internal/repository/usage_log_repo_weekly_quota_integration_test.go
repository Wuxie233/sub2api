//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryRoleWindowSpendAggregatesActualCostOnly(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("quota-role-%s@example.com", uuid.NewString())})
	ownerKeyA := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-owner-a-" + uuid.NewString(), Name: "owner-a"})
	ownerKeyB := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-owner-b-" + uuid.NewString(), Name: "owner-b"})
	guestKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-guest-" + uuid.NewString(), Name: "guest"})
	account := mustCreateAccount(t, client, &service.Account{Name: "quota-role-" + uuid.NewString()})
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	fixture := usageLogIntegrationFixture{ctx: ctx, repo: repo}

	fixture.insert(t, usageLogRow{userID: user.ID, apiKeyID: ownerKeyA.ID, accountID: account.ID, createdAt: start.Add(time.Hour), inputTokens: 10, outputTokens: 20, cacheCreationTokens: 10000, cacheReadTokens: 20000, actualCost: 1.25})
	fixture.insert(t, usageLogRow{userID: user.ID, apiKeyID: ownerKeyB.ID, accountID: account.ID, createdAt: start.Add(2 * time.Hour), inputTokens: 30, outputTokens: 40, actualCost: 2.5})
	fixture.insert(t, usageLogRow{userID: user.ID, apiKeyID: guestKey.ID, accountID: account.ID, createdAt: start.Add(3 * time.Hour), inputTokens: 50, outputTokens: 60, actualCost: 9.75})

	got, err := repo.SumActualCostByAPIKeyIDs(ctx, account.ID, []int64{ownerKeyA.ID, ownerKeyB.ID}, start, end)

	require.NoError(t, err)
	require.InDelta(t, 3.75, got, 1e-9)
}

func TestUsageLogRepositoryWindowBounds(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("quota-bounds-%s@example.com", uuid.NewString())})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-bounds-" + uuid.NewString(), Name: "bounds"})
	account := mustCreateAccount(t, client, &service.Account{Name: "quota-bounds-" + uuid.NewString()})
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	fixture := usageLogIntegrationFixture{ctx: ctx, repo: repo}

	fixture.insert(t, usageLogRow{userID: user.ID, apiKeyID: apiKey.ID, accountID: account.ID, createdAt: start.Add(-time.Nanosecond), inputTokens: 10, outputTokens: 20, actualCost: 100})
	fixture.insert(t, usageLogRow{userID: user.ID, apiKeyID: apiKey.ID, accountID: account.ID, createdAt: start, inputTokens: 10, outputTokens: 20, actualCost: 1.5})
	fixture.insert(t, usageLogRow{userID: user.ID, apiKeyID: apiKey.ID, accountID: account.ID, createdAt: end.Add(-time.Nanosecond), inputTokens: 10, outputTokens: 20, actualCost: 2.25})
	fixture.insert(t, usageLogRow{userID: user.ID, apiKeyID: apiKey.ID, accountID: account.ID, createdAt: end, inputTokens: 10, outputTokens: 20, actualCost: 200})

	keyCost, err := repo.SumActualCostByAPIKeyIDs(ctx, account.ID, []int64{apiKey.ID}, start, end)
	require.NoError(t, err)
	require.InDelta(t, 3.75, keyCost, 1e-9)

	accountCost, err := repo.SumAccountActualCost(ctx, account.ID, start, end)
	require.NoError(t, err)
	require.InDelta(t, 3.75, accountCost, 1e-9)
}

func TestUsageLogRepositoryPulseGuestStatsExcludeCacheTokens(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("quota-guest-%s@example.com", uuid.NewString())})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-guest-stats-" + uuid.NewString(), Name: "guest-stats"})
	account := mustCreateAccount(t, client, &service.Account{Name: "quota-guest-" + uuid.NewString()})
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(7 * 24 * time.Hour)
	fixture := usageLogIntegrationFixture{ctx: ctx, repo: repo}

	fixture.insert(t, usageLogRow{userID: user.ID, apiKeyID: apiKey.ID, accountID: account.ID, createdAt: start.Add(time.Hour), inputTokens: 11, outputTokens: 13, cacheCreationTokens: 10000, cacheReadTokens: 20000, actualCost: 1})
	fixture.insert(t, usageLogRow{userID: user.ID, apiKeyID: apiKey.ID, accountID: account.ID, createdAt: start.Add(2 * time.Hour), inputTokens: 17, outputTokens: 19, cacheCreationTokens: 30000, cacheReadTokens: 40000, actualCost: 1})

	got, err := repo.GuestOwnWindowStats(ctx, apiKey.ID, start, end)

	require.NoError(t, err)
	require.Equal(t, service.GuestOwnStats{RequestCount: 2, InputTokens: 28, OutputTokens: 32}, got)
}

type usageLogIntegrationFixture struct {
	ctx  context.Context
	repo *usageLogRepository
}

type usageLogRow struct {
	userID              int64
	apiKeyID            int64
	accountID           int64
	createdAt           time.Time
	inputTokens         int
	outputTokens        int
	cacheCreationTokens int
	cacheReadTokens     int
	actualCost          float64
}

func (f usageLogIntegrationFixture) insert(t *testing.T, row usageLogRow) {
	t.Helper()
	inserted, err := f.repo.Create(f.ctx, &service.UsageLog{
		UserID:              row.userID,
		APIKeyID:            row.apiKeyID,
		AccountID:           row.accountID,
		RequestID:           uuid.NewString(),
		Model:               "claude-3",
		InputTokens:         row.inputTokens,
		OutputTokens:        row.outputTokens,
		CacheCreationTokens: row.cacheCreationTokens,
		CacheReadTokens:     row.cacheReadTokens,
		TotalCost:           row.actualCost,
		ActualCost:          row.actualCost,
		CreatedAt:           row.createdAt,
	})
	require.NoError(t, err)
	require.True(t, inserted)
}
