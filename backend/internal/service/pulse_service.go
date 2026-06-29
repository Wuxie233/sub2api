package service

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type PulseUsageResult struct {
	AccountLabel string
	Usage        *UsageInfo
}

type PulseService struct {
	cfg          *config.Config
	accountRepo  AccountRepository
	usageService *AccountUsageService
}

func NewPulseService(cfg *config.Config, accountRepo AccountRepository, usageService *AccountUsageService) *PulseService {
	return &PulseService{
		cfg:          cfg,
		accountRepo:  accountRepo,
		usageService: usageService,
	}
}

func (s *PulseService) GetUsage(ctx context.Context, refresh bool) (*PulseUsageResult, error) {
	account, err := s.resolveAccount(ctx)
	if err != nil {
		return nil, err
	}

	var usage *UsageInfo
	if refresh {
		usage, err = s.usageService.GetUsage(ctx, account.ID, true)
		if usage != nil {
			usage.Source = "active"
		}
	} else {
		usage, err = s.usageService.GetPassiveUsage(ctx, account.ID)
	}
	if err != nil {
		return nil, err
	}
	if usage != nil && usage.Source == "" {
		if refresh {
			usage.Source = "active"
		} else {
			usage.Source = "passive"
		}
	}

	return &PulseUsageResult{AccountLabel: account.Name, Usage: usage}, nil
}

func (s *PulseService) resolveAccount(ctx context.Context) (*Account, error) {
	if s.cfg != nil && s.cfg.Pulse.AccountID > 0 {
		return s.accountRepo.GetByID(ctx, s.cfg.Pulse.AccountID)
	}

	accounts, _, err := s.accountRepo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 1000}, PlatformAnthropic, "", "", "", 0, "")
	if err != nil {
		return nil, fmt.Errorf("list Anthropic OAuth accounts: %w", err)
	}

	matched := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account.IsAnthropicOAuthOrSetupToken() {
			matched = append(matched, account)
		}
	}

	switch len(matched) {
	case 0:
		return nil, fmt.Errorf("no Anthropic OAuth account found for Pulse")
	case 1:
		return &matched[0], nil
	default:
		return nil, fmt.Errorf("multiple Anthropic OAuth accounts found; set PULSE_ACCOUNT_ID")
	}
}
