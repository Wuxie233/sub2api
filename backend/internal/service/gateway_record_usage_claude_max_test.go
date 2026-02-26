package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type usageLogRepoRecordUsageStub struct {
	UsageLogRepository

	last     *UsageLog
	inserted bool
	err      error
}

func (s *usageLogRepoRecordUsageStub) Create(_ context.Context, log *UsageLog) (bool, error) {
	copied := *log
	s.last = &copied
	return s.inserted, s.err
}

func newGatewayServiceForRecordUsageTest(repo UsageLogRepository) *GatewayService {
	return &GatewayService{
		usageLogRepo:    repo,
		billingService:  NewBillingService(&config.Config{}, nil),
		cfg:             &config.Config{RunMode: config.RunModeSimple},
		deferredService: &DeferredService{},
	}
}

func TestRecordUsage_SimulateClaudeMaxEnabled_ProjectsAndSkipsTTLOverride(t *testing.T) {
	repo := &usageLogRepoRecordUsageStub{inserted: true}
	svc := newGatewayServiceForRecordUsageTest(repo)

	groupID := int64(11)
	input := &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "req-sim-1",
			Model:     "claude-sonnet-4",
			Duration:  time.Second,
			Usage: ClaudeUsage{
				InputTokens: 160,
			},
		},
		ParsedRequest: &ParsedRequest{
			Model: "claude-sonnet-4",
			Messages: []any{
				map[string]any{
					"role":    "user",
					"content": "please summarize the logs and provide root cause analysis",
				},
			},
		},
		APIKey: &APIKey{
			ID:      1,
			GroupID: &groupID,
			Group: &Group{
				ID:                       groupID,
				Platform:                 PlatformAnthropic,
				RateMultiplier:           1,
				SimulateClaudeMaxEnabled: true,
			},
		},
		User: &User{ID: 2},
		Account: &Account{
			ID:       3,
			Platform: PlatformAnthropic,
			Type:     AccountTypeOAuth,
			Extra: map[string]any{
				"cache_ttl_override_enabled": true,
				"cache_ttl_override_target":  "5m",
			},
		},
	}

	err := svc.RecordUsage(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, repo.last)

	log := repo.last
	total := log.InputTokens + log.CacheCreation5mTokens + log.CacheCreation1hTokens
	require.Equal(t, 160, total, "token 总量应保持不变")
	require.Greater(t, log.CacheCreation1hTokens, 0, "应映射为 1h cache creation")
	require.Equal(t, 0, log.CacheCreation5mTokens, "模拟成功后不应再被 TTL override 改写为 5m")
	require.Equal(t, log.CacheCreation1hTokens, log.CacheCreationTokens, "聚合 cache_creation_tokens 应与 1h 一致")
	require.False(t, log.CacheTTLOverridden, "模拟成功时应跳过 TTL override 标记")
}

func TestRecordUsage_SimulateClaudeMaxDisabled_AppliesTTLOverride(t *testing.T) {
	repo := &usageLogRepoRecordUsageStub{inserted: true}
	svc := newGatewayServiceForRecordUsageTest(repo)

	groupID := int64(12)
	input := &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "req-sim-2",
			Model:     "claude-sonnet-4",
			Duration:  time.Second,
			Usage: ClaudeUsage{
				InputTokens:              40,
				CacheCreationInputTokens: 120,
				CacheCreation1hTokens:    120,
			},
		},
		APIKey: &APIKey{
			ID:      2,
			GroupID: &groupID,
			Group: &Group{
				ID:                       groupID,
				Platform:                 PlatformAnthropic,
				RateMultiplier:           1,
				SimulateClaudeMaxEnabled: false,
			},
		},
		User: &User{ID: 3},
		Account: &Account{
			ID:       4,
			Platform: PlatformAnthropic,
			Type:     AccountTypeOAuth,
			Extra: map[string]any{
				"cache_ttl_override_enabled": true,
				"cache_ttl_override_target":  "5m",
			},
		},
	}

	err := svc.RecordUsage(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, repo.last)

	log := repo.last
	require.Equal(t, 120, log.CacheCreationTokens)
	require.Equal(t, 120, log.CacheCreation5mTokens, "关闭模拟时应执行 TTL override 到 5m")
	require.Equal(t, 0, log.CacheCreation1hTokens)
	require.True(t, log.CacheTTLOverridden, "TTL override 生效时应打标")
}
