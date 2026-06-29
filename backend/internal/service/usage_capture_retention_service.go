package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	usageCaptureRetentionDefaultInterval = 300 * time.Second
	usageCaptureRetentionDefaultBatch    = 2000
)

type UsageCaptureRetentionService struct {
	repo UsageRequestCaptureRepository
	cfg  *config.Config

	running   int32
	startOnce sync.Once
	stopOnce  sync.Once

	workerCtx    context.Context
	workerCancel context.CancelFunc
	workerDone   chan struct{}
}

func NewUsageCaptureRetentionService(repo UsageRequestCaptureRepository, cfg *config.Config) *UsageCaptureRetentionService {
	workerCtx, workerCancel := context.WithCancel(context.Background())
	return &UsageCaptureRetentionService{
		repo:         repo,
		cfg:          cfg,
		workerCtx:    workerCtx,
		workerCancel: workerCancel,
	}
}

func (s *UsageCaptureRetentionService) Start() {
	if s == nil {
		return
	}
	if !s.enabled() {
		logger.LegacyPrintf("service.usage_capture", "[UsageCaptureRetention] not started (disabled)")
		return
	}
	if s.retentionDays() <= 0 {
		logger.LegacyPrintf("service.usage_capture", "[UsageCaptureRetention] not started (retention disabled)")
		return
	}
	if s.repo == nil {
		logger.LegacyPrintf("service.usage_capture", "[UsageCaptureRetention] not started (missing repo)")
		return
	}

	interval := s.retentionInterval()
	s.startOnce.Do(func() {
		s.workerDone = make(chan struct{})
		go s.run(interval)
		logger.LegacyPrintf("service.usage_capture", "[UsageCaptureRetention] started (interval=%s retention_days=%d batch_size=%d)", interval, s.retentionDays(), s.batchSize())
	})
}

func (s *UsageCaptureRetentionService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.workerCancel != nil {
			s.workerCancel()
		}
		if s.workerDone != nil {
			<-s.workerDone
		}
		logger.LegacyPrintf("service.usage_capture", "[UsageCaptureRetention] stopped")
	})
}

func (s *UsageCaptureRetentionService) run(interval time.Duration) {
	defer close(s.workerDone)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.workerCtx.Done():
			return
		case <-ticker.C:
			s.runOnce()
		}
	}
}

func (s *UsageCaptureRetentionService) runOnce() {
	if s == nil || !s.enabled() || s.retentionDays() <= 0 || s.repo == nil {
		return
	}
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		logger.LegacyPrintf("service.usage_capture", "[UsageCaptureRetention] run_once skipped: already_running=true")
		return
	}
	defer atomic.StoreInt32(&s.running, 0)

	ctx := context.Background()
	if s.workerCtx != nil {
		ctx = s.workerCtx
	}
	batchSize := s.batchSize()
	var total int
	for {
		if ctx.Err() != nil {
			logger.LegacyPrintf("service.usage_capture", "[UsageCaptureRetention] run_once interrupted: deleted=%d err=%v", total, ctx.Err())
			return
		}
		deleted, err := s.repo.DeleteExpired(ctx, time.Now(), batchSize)
		if err != nil {
			logger.LegacyPrintf("service.usage_capture", "[UsageCaptureRetention] delete expired failed: deleted=%d err=%v", total, err)
			return
		}
		total += deleted
		if deleted < batchSize {
			logger.LegacyPrintf("service.usage_capture", "[UsageCaptureRetention] run_once done: deleted=%d", total)
			return
		}
	}
}

func (s *UsageCaptureRetentionService) enabled() bool {
	return s != nil && (s.cfg == nil || s.cfg.UsageCapture.Enabled)
}

func (s *UsageCaptureRetentionService) retentionDays() int {
	if s == nil || s.cfg == nil {
		return defaultUsageCaptureRetentionDays
	}
	return s.cfg.UsageCapture.RetentionDays
}

func (s *UsageCaptureRetentionService) retentionInterval() time.Duration {
	if s == nil || s.cfg == nil || s.cfg.UsageCapture.RetentionIntervalSeconds <= 0 {
		return usageCaptureRetentionDefaultInterval
	}
	return time.Duration(s.cfg.UsageCapture.RetentionIntervalSeconds) * time.Second
}

func (s *UsageCaptureRetentionService) batchSize() int {
	if s == nil || s.cfg == nil || s.cfg.UsageCapture.RetentionBatchSize <= 0 {
		return usageCaptureRetentionDefaultBatch
	}
	return s.cfg.UsageCapture.RetentionBatchSize
}
