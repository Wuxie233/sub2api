package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

const (
	antigravityTokenRefreshSkew = 3 * time.Minute
	antigravityTokenCacheSkew   = 5 * time.Minute

	// projectIDFillCooldown 同一账号 project_id 补齐失败后的冷却时间
	projectIDFillCooldown = 60 * time.Second

	// fallbackProjectID 所有获取方式都失败时的兜底值（与 Antigravity-Manager 一致）
	fallbackProjectID = "bamboo-precept-lgxtn"
)

// AntigravityTokenCache Token 缓存接口（复用 GeminiTokenCache 接口定义）
type AntigravityTokenCache = GeminiTokenCache

// AntigravityTokenProvider 管理 Antigravity 账户的 access_token
type AntigravityTokenProvider struct {
	accountRepo             AccountRepository
	tokenCache              AntigravityTokenCache
	antigravityOAuthService *AntigravityOAuthService

	// projectIDFillAttempts 记录每个账号最近一次 project_id 补齐尝试时间，用于冷却去重
	projectIDFillAttempts sync.Map // map[int64]time.Time
}

func NewAntigravityTokenProvider(
	accountRepo AccountRepository,
	tokenCache AntigravityTokenCache,
	antigravityOAuthService *AntigravityOAuthService,
) *AntigravityTokenProvider {
	return &AntigravityTokenProvider{
		accountRepo:             accountRepo,
		tokenCache:              tokenCache,
		antigravityOAuthService: antigravityOAuthService,
	}
}

// GetAccessToken 获取有效的 access_token
func (p *AntigravityTokenProvider) GetAccessToken(ctx context.Context, account *Account) (string, error) {
	if account == nil {
		return "", errors.New("account is nil")
	}
	if account.Platform != PlatformAntigravity {
		return "", errors.New("not an antigravity account")
	}
	// upstream 类型：直接从 credentials 读取 api_key，不走 OAuth 刷新流程
	if account.Type == AccountTypeUpstream {
		apiKey := account.GetCredential("api_key")
		if apiKey == "" {
			return "", errors.New("upstream account missing api_key in credentials")
		}
		return apiKey, nil
	}
	if account.Type != AccountTypeOAuth {
		return "", errors.New("not an antigravity oauth account")
	}

	cacheKey := AntigravityTokenCacheKey(account)

	// 1. 先尝试缓存
	if p.tokenCache != nil {
		if token, err := p.tokenCache.GetAccessToken(ctx, cacheKey); err == nil && strings.TrimSpace(token) != "" {
			return token, nil
		}
	}

	// 2. 如果即将过期则刷新
	expiresAt := account.GetCredentialAsTime("expires_at")
	needsRefresh := expiresAt == nil || time.Until(*expiresAt) <= antigravityTokenRefreshSkew
	if needsRefresh && p.tokenCache != nil {
		locked, err := p.tokenCache.AcquireRefreshLock(ctx, cacheKey, 30*time.Second)
		if err == nil && locked {
			defer func() { _ = p.tokenCache.ReleaseRefreshLock(ctx, cacheKey) }()

			// 拿到锁后再次检查缓存（另一个 worker 可能已刷新）
			if token, err := p.tokenCache.GetAccessToken(ctx, cacheKey); err == nil && strings.TrimSpace(token) != "" {
				return token, nil
			}

			// 从数据库获取最新账户信息
			fresh, err := p.accountRepo.GetByID(ctx, account.ID)
			if err == nil && fresh != nil {
				account = fresh
			}
			expiresAt = account.GetCredentialAsTime("expires_at")
			if expiresAt == nil || time.Until(*expiresAt) <= antigravityTokenRefreshSkew {
				if p.antigravityOAuthService == nil {
					return "", errors.New("antigravity oauth service not configured")
				}
				tokenInfo, err := p.antigravityOAuthService.RefreshAccountToken(ctx, account)
				if err != nil {
					return "", err
				}
				newCredentials := p.antigravityOAuthService.BuildAccountCredentials(tokenInfo)
				mergeCredentials(newCredentials, account.Credentials)
				account.Credentials = newCredentials
				if updateErr := p.accountRepo.Update(ctx, account); updateErr != nil {
					slog.Error("failed to update account credentials after token refresh", "account_id", account.ID, "error", updateErr)
				}
				expiresAt = account.GetCredentialAsTime("expires_at")
			}
		}
	}

	accessToken := account.GetCredential("access_token")
	if strings.TrimSpace(accessToken) == "" {
		return "", errors.New("access_token not found in credentials")
	}

	// 3. 如果缺少 project_id，轻量补齐（不刷新 token）
	if strings.TrimSpace(account.GetCredential("project_id")) == "" {
		p.tryFillProjectID(ctx, account, accessToken)
	}

	// 4. 存入缓存（验证版本后再写入，避免异步刷新任务与请求线程的竞态条件）
	if p.tokenCache != nil {
		latestAccount, isStale := CheckTokenVersion(ctx, account, p.accountRepo)
		if isStale && latestAccount != nil {
			// 版本过时，使用 DB 中的最新 token
			slog.Debug("antigravity_token_version_stale_use_latest", "account_id", account.ID)
			accessToken = latestAccount.GetCredential("access_token")
			if strings.TrimSpace(accessToken) == "" {
				return "", errors.New("access_token not found after version check")
			}
			// 不写入缓存，让下次请求重新处理
		} else {
			ttl := 30 * time.Minute
			if expiresAt != nil {
				until := time.Until(*expiresAt)
				switch {
				case until > antigravityTokenCacheSkew:
					ttl = until - antigravityTokenCacheSkew
				case until > 0:
					ttl = until
				default:
					ttl = time.Minute
				}
			}
			_ = p.tokenCache.SetAccessToken(ctx, cacheKey, accessToken, ttl)
		}
	}

	return accessToken, nil
}

// tryFillProjectID 轻量级 project_id 补齐（与 Antigravity-Manager 保持一致）
// 只调用 loadCodeAssist + onboardUser，不刷新 token。
// 带冷却去重：同一账号 60s 内不重复尝试。
func (p *AntigravityTokenProvider) tryFillProjectID(ctx context.Context, account *Account, accessToken string) {
	// 冷却检查：60s 内不重复尝试
	if lastAttempt, ok := p.projectIDFillAttempts.Load(account.ID); ok {
		if t, ok := lastAttempt.(time.Time); ok && time.Since(t) < projectIDFillCooldown {
			return
		}
	}
	p.projectIDFillAttempts.Store(account.ID, time.Now())

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	client := antigravity.NewClient(proxyURL)

	// Step 1: loadCodeAssist（单次调用，不重试）
	loadResp, loadRaw, err := client.LoadCodeAssist(ctx, accessToken)
	if err == nil && loadResp != nil && loadResp.CloudAICompanionProject != "" {
		p.persistProjectID(ctx, account, loadResp.CloudAICompanionProject)
		p.projectIDFillAttempts.Delete(account.ID) // 成功后清除冷却
		return
	}

	// Step 2: onboardUser（loadCodeAssist 成功但未返回 project_id 时）
	if err == nil {
		if projectID, onboardErr := tryOnboardProjectID(ctx, client, accessToken, loadRaw); onboardErr == nil && projectID != "" {
			p.persistProjectID(ctx, account, projectID)
			p.projectIDFillAttempts.Delete(account.ID)
			return
		}
	}

	// Step 3: 兜底值（与 Antigravity-Manager 一致）
	slog.Warn("project_id fill failed, using fallback",
		"account_id", account.ID,
		"fallback", fallbackProjectID,
	)
	p.persistProjectID(ctx, account, fallbackProjectID)
}

// persistProjectID 将 project_id 写入账号凭证并持久化
func (p *AntigravityTokenProvider) persistProjectID(ctx context.Context, account *Account, projectID string) {
	account.Credentials["project_id"] = projectID
	if p.accountRepo == nil {
		return
	}
	if updateErr := p.accountRepo.Update(ctx, account); updateErr != nil {
		slog.Error("failed to persist project_id", "account_id", account.ID, "error", updateErr)
	}
}

// mergeCredentials 将 old 中不存在于 new 的字段合并到 new
func mergeCredentials(newCreds, oldCreds map[string]any) {
	for k, v := range oldCreds {
		if _, exists := newCreds[k]; !exists {
			newCreds[k] = v
		}
	}
}

func AntigravityTokenCacheKey(account *Account) string {
	projectID := strings.TrimSpace(account.GetCredential("project_id"))
	if projectID != "" {
		return "ag:" + projectID
	}
	return "ag:account:" + strconv.FormatInt(account.ID, 10)
}
