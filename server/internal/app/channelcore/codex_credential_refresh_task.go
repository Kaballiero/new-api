package channelcore

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/QuantumNous/new-api/common"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/refresh_codex_credentials"
	"github.com/QuantumNous/new-api/logger"
)

const (
	codexCredentialRefreshTickInterval = 10 * time.Minute
	codexCredentialRefreshThreshold    = 24 * time.Hour
)

var (
	codexCredentialRefreshOnce    sync.Once
	codexCredentialRefreshRunning atomic.Bool
)

func StartCodexCredentialAutoRefreshTask() {
	codexCredentialRefreshOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}

		gopool.Go(func() {
			logger.LogInfo(
				context.Background(),
				fmt.Sprintf(
					"codex credential auto-refresh task started: tick=%s threshold=%s",
					codexCredentialRefreshTickInterval,
					codexCredentialRefreshThreshold,
				),
			)

			ticker := time.NewTicker(codexCredentialRefreshTickInterval)
			defer ticker.Stop()

			runCodexCredentialAutoRefreshOnce()
			for range ticker.C {
				runCodexCredentialAutoRefreshOnce()
			}
		})
	})
}

func runCodexCredentialAutoRefreshOnce() {
	if !codexCredentialRefreshRunning.CompareAndSwap(false, true) {
		return
	}
	defer codexCredentialRefreshRunning.Store(false)
	scenario := refresh_codex_credentials.NewUsecase(
		channelstore.NewStore(),
		time.Now,
		func(
			ctx context.Context,
			id int,
		) (*channeldomain.CodexOAuthKey, error) {
			key, _, err := RefreshCodexChannelCredential(ctx, id, CodexCredentialRefreshOptions{ResetCaches: false})
			return key, err
		},
		channelstore.InitChannelCache,
		func() bool { return common.DebugEnabled },
		func(level, message string) {
			ctx := context.Background()
			switch level {
			case "error":
				logger.LogError(ctx, message)
			case "warn":
				logger.LogWarn(ctx, message)
			case "info":
				logger.LogInfo(ctx, message)
			case "debug":
				logger.LogDebug(ctx, message)
			}
		},
	)
	scenario.Execute()
}
