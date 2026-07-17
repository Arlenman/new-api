package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
)

const (
	tokenQuotaResetTickInterval = time.Minute
	tokenQuotaResetBatchSize    = 300
)

var (
	tokenQuotaResetOnce    sync.Once
	tokenQuotaResetRunning atomic.Bool
)

func StartTokenQuotaResetTask() {
	tokenQuotaResetOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("token quota reset task started: tick=%s", tokenQuotaResetTickInterval))
			ticker := time.NewTicker(tokenQuotaResetTickInterval)
			defer ticker.Stop()

			runTokenQuotaResetOnce()
			for range ticker.C {
				runTokenQuotaResetOnce()
			}
		})
	})
}

func runTokenQuotaResetOnce() {
	if !tokenQuotaResetRunning.CompareAndSwap(false, true) {
		return
	}
	defer tokenQuotaResetRunning.Store(false)

	resetCount, err := model.ResetDueTokenQuotas(common.GetTimestamp(), tokenQuotaResetBatchSize)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("token quota reset task failed: %v", err))
		return
	}
	if common.DebugEnabled && resetCount > 0 {
		logger.LogDebug(context.Background(), "token quota reset task: reset_count=%d", resetCount)
	}
}
