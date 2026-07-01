package config

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	PulseAccessRoleOwner = "owner"
	PulseAccessRoleGuest = "guest"
)

func (c *Config) ResolveKeyRole(accountID, apiKeyID int64) (string, bool) {
	car, ok := c.CarByAccountID(accountID)
	if !ok {
		return "", false
	}
	if containsInt64(car.OwnerKeyIDs, apiKeyID) {
		return PulseAccessRoleOwner, true
	}
	if containsInt64(car.GuestKeyIDs, apiKeyID) {
		return PulseAccessRoleGuest, true
	}
	return "", false
}

func (c *Config) CarByAccountID(accountID int64) (*WeeklyQuotaCarConfig, bool) {
	for index := range c.WeeklyQuota.Cars {
		if c.WeeklyQuota.Cars[index].AccountID == accountID {
			return &c.WeeklyQuota.Cars[index], true
		}
	}
	return nil, false
}

func (c *Config) ResolvePulseIdentity(presentedToken string) (*PulseAccessTokenConfig, bool) {
	presentedHash := sha256.Sum256([]byte(presentedToken))
	presentedHashHex := hex.EncodeToString(presentedHash[:])
	for index := range c.Pulse.AccessTokens {
		configuredHash := c.Pulse.AccessTokens[index].TokenSHA256
		if subtle.ConstantTimeCompare([]byte(presentedHashHex), []byte(configuredHash)) == 1 {
			return &c.Pulse.AccessTokens[index], true
		}
	}
	return nil, false
}

func applyJSONEnvOverrides(cfg *Config) error {
	if raw := strings.TrimSpace(os.Getenv("WEEKLY_QUOTA_CARS_JSON")); raw != "" {
		var cars []WeeklyQuotaCarConfig
		if err := json.Unmarshal([]byte(raw), &cars); err != nil {
			return fmt.Errorf("parse WEEKLY_QUOTA_CARS_JSON: %w", err)
		}
		cfg.WeeklyQuota.Cars = cars
	}
	if raw := strings.TrimSpace(os.Getenv("PULSE_ACCESS_TOKENS_JSON")); raw != "" {
		var tokens []PulseAccessTokenConfig
		if err := json.Unmarshal([]byte(raw), &tokens); err != nil {
			return fmt.Errorf("parse PULSE_ACCESS_TOKENS_JSON: %w", err)
		}
		cfg.Pulse.AccessTokens = tokens
	}
	return nil
}

func applyWeeklyQuotaDefaults(cfg *WeeklyQuotaConfig) {
	for index := range cfg.Cars {
		car := &cfg.Cars[index]
		if car.GuestShareBps == 0 {
			car.GuestShareBps = 6600
		}
		if car.OwnerShareBps == 0 {
			car.OwnerShareBps = 3400
		}
		if car.MinCalibrationUtilization == 0 {
			car.MinCalibrationUtilization = 0.08
		}
		if car.PassiveSampleMaxAgeSeconds == 0 {
			car.PassiveSampleMaxAgeSeconds = 900
		}
		if car.GuestStopUtilization == 0 {
			car.GuestStopUtilization = 0.97
		}
		if car.OwnerStopUtilization == 0 {
			car.OwnerStopUtilization = 0.985
		}
		if car.DefaultReserveUSD == 0 {
			car.DefaultReserveUSD = 0.5
		}
	}
}

func normalizePulseAccessTokens(tokens []PulseAccessTokenConfig) {
	for index := range tokens {
		tokens[index].TokenSHA256 = strings.TrimSpace(tokens[index].TokenSHA256)
		tokens[index].Role = strings.ToLower(strings.TrimSpace(tokens[index].Role))
		tokens[index].Label = strings.TrimSpace(tokens[index].Label)
	}
}

func validateWeeklyQuotaConfig(cfg WeeklyQuotaConfig) error {
	if !cfg.Enabled {
		return nil
	}
	for index, car := range cfg.Cars {
		if car.AccountID <= 0 {
			return fmt.Errorf("weekly_quota.cars[%d].account_id must be positive", index)
		}
		if car.BudgetSeedUSD <= 0 {
			return fmt.Errorf("weekly_quota.cars[%d].budget_seed_usd must be positive when weekly_quota.enabled=true", index)
		}
		if car.OwnerShareBps+car.GuestShareBps != 10000 {
			return fmt.Errorf("weekly_quota.cars[%d].owner_share_bps + guest_share_bps must equal 10000", index)
		}
	}
	return nil
}

func validatePulseAccessTokens(tokens []PulseAccessTokenConfig) error {
	for index, token := range tokens {
		if !isLowerHexSHA256(token.TokenSHA256) {
			return fmt.Errorf("pulse.access_tokens[%d].token_sha256 must be a 64-char lowercase hex sha256 hash", index)
		}
		switch token.Role {
		case PulseAccessRoleOwner, PulseAccessRoleGuest:
		default:
			return fmt.Errorf("pulse.access_tokens[%d].role must be owner or guest", index)
		}
		if token.AccountID <= 0 {
			return fmt.Errorf("pulse.access_tokens[%d].account_id must be positive", index)
		}
		if token.Role == PulseAccessRoleGuest && token.APIKeyID <= 0 {
			return fmt.Errorf("pulse.access_tokens[%d].api_key_id must be positive for guest tokens", index)
		}
	}
	return nil
}

func isLowerHexSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func containsInt64(values []int64, needle int64) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
