package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type pulseAccountRepoStub struct {
	AccountRepository

	accounts          map[int64]*Account
	listWithFilters   []Account
	lastPlatform      string
	lastAccountType   string
	lastPageSize      int
	listWithFiltersOK bool
}

func (r *pulseAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	account, ok := r.accounts[id]
	if !ok {
		return nil, ErrAccountNotFound
	}
	return account, nil
}

func (r *pulseAccountRepoStub) ListWithFilters(_ context.Context, params pagination.PaginationParams, platform, accountType, _ string, _ string, _ int64, _ string) ([]Account, *pagination.PaginationResult, error) {
	r.listWithFiltersOK = true
	r.lastPlatform = platform
	r.lastAccountType = accountType
	r.lastPageSize = params.PageSize
	return r.listWithFilters, &pagination.PaginationResult{Total: int64(len(r.listWithFilters))}, nil
}

func TestPulseService_ExplicitAccountIDResolvesThatAccount(t *testing.T) {
	t.Parallel()

	repo := &pulseAccountRepoStub{
		accounts: map[int64]*Account{
			42: {ID: 42, Name: "primary", Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		},
	}
	svc := NewPulseService(&config.Config{Pulse: config.PulseConfig{AccountID: 42}}, repo, nil)

	account, err := svc.resolveAccount(context.Background())
	if err != nil {
		t.Fatalf("resolveAccount() error = %v", err)
	}
	if account.ID != 42 {
		t.Fatalf("resolveAccount() ID = %d, want 42", account.ID)
	}
	if repo.listWithFiltersOK {
		t.Fatal("explicit account_id must not list accounts")
	}
}

func TestPulseService_AutoPicksExactlyOneAnthropicOAuthAccount(t *testing.T) {
	t.Parallel()

	repo := &pulseAccountRepoStub{
		listWithFilters: []Account{
			{ID: 7, Name: "solo", Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		},
	}
	svc := NewPulseService(&config.Config{}, repo, nil)

	account, err := svc.resolveAccount(context.Background())
	if err != nil {
		t.Fatalf("resolveAccount() error = %v", err)
	}
	if account.ID != 7 {
		t.Fatalf("resolveAccount() ID = %d, want 7", account.ID)
	}
	if repo.lastPlatform != PlatformAnthropic {
		t.Fatalf("ListWithFilters platform = %q, want %q", repo.lastPlatform, PlatformAnthropic)
	}
	if repo.lastAccountType != "" {
		t.Fatalf("ListWithFilters accountType = %q, want empty before helper filtering", repo.lastAccountType)
	}
	if repo.lastPageSize != 1000 {
		t.Fatalf("ListWithFilters page size = %d, want 1000", repo.lastPageSize)
	}
}

func TestPulseService_AutoPickNoAccountsErrors(t *testing.T) {
	t.Parallel()

	svc := NewPulseService(&config.Config{}, &pulseAccountRepoStub{}, nil)

	_, err := svc.resolveAccount(context.Background())
	if err == nil {
		t.Fatal("resolveAccount() expected error")
	}
	if !strings.Contains(err.Error(), "Anthropic OAuth") {
		t.Fatalf("resolveAccount() error = %q, want Anthropic OAuth hint", err.Error())
	}
}

func TestPulseService_AutoPickMultipleAccountsRequiresConfig(t *testing.T) {
	t.Parallel()

	repo := &pulseAccountRepoStub{
		listWithFilters: []Account{
			{ID: 1, Name: "first", Platform: PlatformAnthropic, Type: AccountTypeOAuth},
			{ID: 2, Name: "second", Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		},
	}
	svc := NewPulseService(&config.Config{}, repo, nil)

	_, err := svc.resolveAccount(context.Background())
	if err == nil {
		t.Fatal("resolveAccount() expected error")
	}
	if !strings.Contains(err.Error(), "PULSE_ACCOUNT_ID") {
		t.Fatalf("resolveAccount() error = %q, want PULSE_ACCOUNT_ID hint", err.Error())
	}
}
