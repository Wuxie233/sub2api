package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestLoadWeeklyQuotaCarsFromYAML(t *testing.T) {
	// Given
	resetViperWithJWTSecret(t)
	configDir := t.TempDir()
	writeConfigFile(t, configDir, `
weekly_quota:
  enabled: true
  cars:
    - account_id: 9
      budget_seed_usd: 2248
      owner_key_ids: [1, 3]
      guest_key_ids: [23, 24]
`)
	t.Setenv("DATA_DIR", configDir)

	// When
	cfg, err := Load()

	// Then
	require.NoError(t, err)
	require.True(t, cfg.WeeklyQuota.Enabled)
	require.Len(t, cfg.WeeklyQuota.Cars, 1)
	car := cfg.WeeklyQuota.Cars[0]
	require.Equal(t, int64(9), car.AccountID)
	require.Equal(t, 2248.0, car.BudgetSeedUSD)
	require.Equal(t, 6600, car.GuestShareBps)
	require.Equal(t, 3400, car.OwnerShareBps)
	require.Equal(t, 0.08, car.MinCalibrationUtilization)
	require.Equal(t, 900, car.PassiveSampleMaxAgeSeconds)
	require.Equal(t, 0.97, car.GuestStopUtilization)
	require.Equal(t, 0.985, car.OwnerStopUtilization)
	require.Equal(t, 0.5, car.DefaultReserveUSD)
	require.Equal(t, []int64{1, 3}, car.OwnerKeyIDs)
	require.Equal(t, []int64{23, 24}, car.GuestKeyIDs)
}

func TestLoadWeeklyQuotaCarsFromEnvJSON(t *testing.T) {
	// Given
	resetViperWithJWTSecret(t)
	t.Setenv("WEEKLY_QUOTA_ENABLED", "true")
	t.Setenv("WEEKLY_QUOTA_CARS_JSON", `[{"account_id":9,"budget_seed_usd":2248,"owner_key_ids":[1,3],"guest_key_ids":[23,24]}]`)

	// When
	cfg, err := Load()

	// Then
	require.NoError(t, err)
	require.True(t, cfg.WeeklyQuota.Enabled)
	require.Len(t, cfg.WeeklyQuota.Cars, 1)
	require.Equal(t, int64(9), cfg.WeeklyQuota.Cars[0].AccountID)
	require.Equal(t, []int64{23, 24}, cfg.WeeklyQuota.Cars[0].GuestKeyIDs)
}

func TestValidateWeeklyQuotaRejectsShareTotalMismatch(t *testing.T) {
	// Given
	cfg := loadValidConfigForWeeklyQuota(t)
	cfg.WeeklyQuota.Enabled = true
	cfg.WeeklyQuota.Cars = []WeeklyQuotaCarConfig{{
		AccountID:     9,
		BudgetSeedUSD: 2248,
		GuestShareBps: 6600,
		OwnerShareBps: 3300,
		OwnerKeyIDs:   []int64{1, 3},
		GuestKeyIDs:   []int64{23, 24},
	}}

	// When
	err := cfg.Validate()

	// Then
	require.Error(t, err)
	require.Contains(t, err.Error(), "weekly_quota.cars[0].owner_share_bps + guest_share_bps")
}

func TestValidatePulseAccessTokenRejectsRawToken(t *testing.T) {
	// Given
	cfg := loadValidConfigForWeeklyQuota(t)
	cfg.Pulse.AccessTokens = []PulseAccessTokenConfig{{
		TokenSHA256: "raw-token-value",
		Role:        "guest",
		AccountID:   9,
		APIKeyID:    23,
		Label:       "guest-c",
	}}

	// When
	err := cfg.Validate()

	// Then
	require.Error(t, err)
	require.Contains(t, err.Error(), "pulse.access_tokens[0].token_sha256")
}

func TestLoadPulseAccessTokensFromEnvJSON(t *testing.T) {
	// Given
	resetViperWithJWTSecret(t)
	t.Setenv("PULSE_ACCESS_TOKENS_JSON", `[{"token_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","role":"guest","account_id":9,"api_key_id":23,"label":"guest-c"}]`)

	// When
	cfg, err := Load()

	// Then
	require.NoError(t, err)
	require.Len(t, cfg.Pulse.AccessTokens, 1)
	require.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", cfg.Pulse.AccessTokens[0].TokenSHA256)
	require.Equal(t, "guest", cfg.Pulse.AccessTokens[0].Role)
	require.Equal(t, int64(9), cfg.Pulse.AccessTokens[0].AccountID)
	require.Equal(t, int64(23), cfg.Pulse.AccessTokens[0].APIKeyID)
	require.Equal(t, "guest-c", cfg.Pulse.AccessTokens[0].Label)
}

func TestValidatePulseAccessTokenRejectsGuestWithoutAPIKeyID(t *testing.T) {
	// Given
	cfg := loadValidConfigForWeeklyQuota(t)
	cfg.Pulse.AccessTokens = []PulseAccessTokenConfig{{
		TokenSHA256: strings.Repeat("a", 64),
		Role:        "guest",
		AccountID:   9,
		Label:       "guest-c",
	}}

	// When
	err := cfg.Validate()

	// Then
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key_id")
}

func TestResolveKeyRoleMapsManagedOwnerGuestAndUnmanaged(t *testing.T) {
	// Given
	cfg := &Config{WeeklyQuota: WeeklyQuotaConfig{Cars: []WeeklyQuotaCarConfig{{
		AccountID:   9,
		OwnerKeyIDs: []int64{1, 3},
		GuestKeyIDs: []int64{23, 24},
	}}}}

	// When
	guestRole, guestManaged := cfg.ResolveKeyRole(9, 23)
	ownerRole, ownerManaged := cfg.ResolveKeyRole(9, 1)
	unknownRole, unknownManaged := cfg.ResolveKeyRole(9, 99)

	// Then
	require.Equal(t, "guest", guestRole)
	require.True(t, guestManaged)
	require.Equal(t, "owner", ownerRole)
	require.True(t, ownerManaged)
	require.Empty(t, unknownRole)
	require.False(t, unknownManaged)
}

func TestResolvePulseIdentityMatchesHashAndRejectsWrongToken(t *testing.T) {
	// Given
	presentedToken := randomPulseTokenFixture(t)
	wrongToken := randomPulseTokenFixture(t)
	hashBytes := sha256.Sum256([]byte(presentedToken))
	cfg := &Config{Pulse: PulseConfig{AccessTokens: []PulseAccessTokenConfig{{
		TokenSHA256: hex.EncodeToString(hashBytes[:]),
		Role:        "guest",
		AccountID:   9,
		APIKeyID:    23,
		Label:       "guest-c",
	}}}}

	// When
	identity, matched := cfg.ResolvePulseIdentity(presentedToken)
	wrongIdentity, wrongMatched := cfg.ResolvePulseIdentity(wrongToken)

	// Then
	require.True(t, matched)
	require.Equal(t, "guest-c", identity.Label)
	require.Equal(t, int64(9), identity.AccountID)
	require.Equal(t, int64(23), identity.APIKeyID)
	require.False(t, wrongMatched)
	require.Nil(t, wrongIdentity)
}

func loadValidConfigForWeeklyQuota(t *testing.T) *Config {
	t.Helper()
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	return cfg
}

func writeConfigFile(t *testing.T, dir string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(strings.TrimSpace(content)+"\n"), 0o600))
	viper.Reset()
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
}

func randomPulseTokenFixture(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 32)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return hex.EncodeToString(buf)
}
