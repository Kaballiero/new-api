package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.RealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse), nil
	}

	info.IsStream = true
	clientConn := info.ClientWs
	targetConn := info.TargetWs

	done := make(chan struct{})
	result := make(chan error, 2)
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			close(done)
			_ = targetConn.Close()
			_ = clientConn.Close()
		})
	}
	var readers sync.WaitGroup
	var localMu sync.Mutex
	localUsage := dto.RealtimeUsage{}
	sumUsage := &dto.RealtimeUsage{}

	consume := func(usage *dto.RealtimeUsage) error {
		if usage == nil {
			return nil
		}
		if err := service.ValidateRealtimeUsage(usage); err != nil {
			return err
		}
		if usage.TotalTokens == 0 {
			return nil
		}
		return preConsumeUsage(c, info, usage, sumUsage)
	}

	readers.Add(1)
	go func() {
		defer readers.Done()
		var readerErr error
		defer func() {
			if r := recover(); r != nil {
				readerErr = fmt.Errorf("panic in client reader: %v", r)
			}
			result <- readerErr
		}()
		for {
			select {
			case <-done:
				return
			default:
				_, message, err := clientConn.ReadMessage()
				if err != nil {
					select {
					case <-done:
						return
					default:
					}
					if isNormalRealtimeClose(err) {
						return
					}
					readerErr = fmt.Errorf("error reading client websocket: %w", err)
					return
				}

				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					readerErr = fmt.Errorf("error unmarshalling client message: %w", err)
					return
				}

				if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdate {
					if realtimeEvent.Session != nil {
						if realtimeEvent.Session.Tools != nil {
							info.RealtimeTools = realtimeEvent.Session.Tools
						}
					}
				}

				textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
				if err != nil {
					readerErr = fmt.Errorf("error counting client text token: %w", err)
					return
				}
				logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
				localMu.Lock()
				localUsage.TotalTokens += textToken + audioToken
				localUsage.InputTokens += textToken + audioToken
				localUsage.InputTokenDetails.TextTokens += textToken
				localUsage.InputTokenDetails.AudioTokens += audioToken
				localMu.Unlock()

				err = helper.WssString(c, targetConn, string(message))
				if err != nil {
					readerErr = fmt.Errorf("error writing to target: %w", err)
					return
				}
			}
		}
	}()

	readers.Add(1)
	go func() {
		defer readers.Done()
		var readerErr error
		defer func() {
			if r := recover(); r != nil {
				readerErr = fmt.Errorf("panic in target reader: %v", r)
			}
			result <- readerErr
		}()
		for {
			select {
			case <-done:
				return
			default:
				_, message, err := targetConn.ReadMessage()
				if err != nil {
					select {
					case <-done:
						return
					default:
					}
					if isNormalRealtimeClose(err) {
						return
					}
					readerErr = fmt.Errorf("error reading target websocket: %w", err)
					return
				}
				info.SetFirstResponseTime()
				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					readerErr = fmt.Errorf("error unmarshalling target message: %w", err)
					return
				}

				if realtimeEvent.Type == dto.RealtimeEventTypeResponseDone {
					realtimeUsage := realtimeEvent.Response.Usage
					var contribution dto.RealtimeUsage
					if realtimeUsage != nil {
						contribution = *realtimeUsage
						localMu.Lock()
						localUsage = dto.RealtimeUsage{}
						localMu.Unlock()
					} else {
						textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
						if err != nil {
							readerErr = fmt.Errorf("error counting target text token: %w", err)
							return
						}
						logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
						localMu.Lock()
						localUsage.TotalTokens += textToken + audioToken
						info.IsFirstRequest = false
						localUsage.InputTokens += textToken + audioToken
						localUsage.InputTokenDetails.TextTokens += textToken
						localUsage.InputTokenDetails.AudioTokens += audioToken
						contribution = localUsage
						localUsage = dto.RealtimeUsage{}
						localMu.Unlock()
					}
					if err := consume(&contribution); err != nil {
						readerErr = fmt.Errorf("error consume usage: %w", err)
						return
					}
					logger.LogInfo(c, fmt.Sprintf("realtime streaming sumUsage: %v", sumUsage))
					logger.LogInfo(c, fmt.Sprintf("realtime streaming localUsage: %v", localUsage))
					logger.LogInfo(c, fmt.Sprintf("realtime streaming localUsage: %v", localUsage))

				} else if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdated || realtimeEvent.Type == dto.RealtimeEventTypeSessionCreated {
					realtimeSession := realtimeEvent.Session
					if realtimeSession != nil {
						// update audio format
						info.InputAudioFormat = common.GetStringIfEmpty(realtimeSession.InputAudioFormat, info.InputAudioFormat)
						info.OutputAudioFormat = common.GetStringIfEmpty(realtimeSession.OutputAudioFormat, info.OutputAudioFormat)
					}
				} else {
					textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
					if err != nil {
						readerErr = fmt.Errorf("error counting target text token: %w", err)
						return
					}
					logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
					localMu.Lock()
					localUsage.TotalTokens += textToken + audioToken
					localUsage.OutputTokens += textToken + audioToken
					localUsage.OutputTokenDetails.TextTokens += textToken
					localUsage.OutputTokenDetails.AudioTokens += audioToken
					localMu.Unlock()
				}

				err = helper.WssString(c, clientConn, string(message))
				if err != nil {
					readerErr = fmt.Errorf("error writing to client: %w", err)
					return
				}
			}
		}
	}()

	var terminalErr error
	remainingReaderResults := 2
	select {
	case terminalErr = <-result:
		remainingReaderResults--
	case <-c.Done():
		terminalErr = c.Err()
		if terminalErr == nil {
			terminalErr = context.Canceled
		}
	}
	stop()
	readers.Wait()
	for range remainingReaderResults {
		terminalErr = errors.Join(terminalErr, <-result)
	}
	localMu.Lock()
	tail := localUsage
	localUsage = dto.RealtimeUsage{}
	localMu.Unlock()
	if err := consume(&tail); err != nil {
		terminalErr = errors.Join(terminalErr, fmt.Errorf("error consume terminal usage: %w", err))
	}
	if terminalErr != nil {
		logger.LogError(c, "realtime error: "+terminalErr.Error())
	}

	if terminalErr != nil {
		return types.NewError(terminalErr, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry()), sumUsage
	}
	return nil, sumUsage
}

func isNormalRealtimeClose(err error) bool {
	return errors.Is(err, io.EOF) || websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway)
}

func preConsumeUsage(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage, totalUsage *dto.RealtimeUsage) error {
	if usage == nil || totalUsage == nil {
		return fmt.Errorf("invalid usage pointer")
	}
	if err := service.ValidateRealtimeUsage(usage); err != nil {
		return err
	}

	usedVars := map[string]bool{}
	if snap := info.TieredBillingSnapshot; snap != nil {
		usedVars = billingexpr.UsedVars(snap.ExprString)
		if reason := service.RealtimeCacheDiscountIssue(usage, usedVars); reason != "" {
			usage.CacheDiscountUnavailable = true
			usage.CacheDiscountUnavailableReason = reason
			usage.InputTokenDetails.CachedTokens = 0
			usage.InputTokenDetails.CachedTokensDetails = nil
		}
	}

	candidate := cloneRealtimeUsage(totalUsage)
	if !addRealtimeUsage(candidate, usage, usedVars) {
		return fmt.Errorf("realtime usage aggregate overflow")
	}
	if usage.InputTokenDetails.CachedTokens > 0 && usedVars["cr"] && (usedVars["ai"] || usedVars["img"]) {
		if !mergeCachedTokenDetails(&candidate.InputTokenDetails, usage.InputTokenDetails.CachedTokensDetails, usedVars) {
			return fmt.Errorf("realtime cached usage aggregate overflow")
		}
	}
	if usage.CacheDiscountUnavailable {
		candidate.CacheDiscountUnavailable = true
		if candidate.CacheDiscountUnavailableReason == "" {
			candidate.CacheDiscountUnavailableReason = usage.CacheDiscountUnavailableReason
		}
	}
	*totalUsage = *candidate
	// The observation belongs to this event even if its provisional debit is
	// refused. Callers clear their contribution before returning the error so a
	// terminal tail cannot count it a second time.
	return service.PreWssConsumeQuota(ctx, info, usage)
}

func cloneRealtimeUsage(usage *dto.RealtimeUsage) *dto.RealtimeUsage {
	clone := *usage
	if details := usage.InputTokenDetails.CachedTokensDetails; details != nil {
		copy := *details
		copy.TextTokens = cloneRealtimeTokenPointer(details.TextTokens)
		copy.AudioTokens = cloneRealtimeTokenPointer(details.AudioTokens)
		copy.ImageTokens = cloneRealtimeTokenPointer(details.ImageTokens)
		clone.InputTokenDetails.CachedTokensDetails = &copy
	}
	return &clone
}

func cloneRealtimeTokenPointer(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func addRealtimeUsage(total, usage *dto.RealtimeUsage, usedVars map[string]bool) bool {
	return addRealtimeToken(&total.TotalTokens, usage.TotalTokens) &&
		addRealtimeToken(&total.InputTokens, usage.InputTokens) &&
		addRealtimeToken(&total.OutputTokens, usage.OutputTokens) &&
		addRealtimeToken(&total.InputTokenDetails.TextTokens, usage.InputTokenDetails.TextTokens) &&
		addRealtimeToken(&total.InputTokenDetails.AudioTokens, usage.InputTokenDetails.AudioTokens) &&
		addRealtimeToken(&total.InputTokenDetails.ImageTokens, usage.InputTokenDetails.ImageTokens) &&
		addRealtimeToken(&total.OutputTokenDetails.TextTokens, usage.OutputTokenDetails.TextTokens) &&
		addRealtimeToken(&total.OutputTokenDetails.AudioTokens, usage.OutputTokenDetails.AudioTokens) &&
		addRealtimeToken(&total.OutputTokenDetails.ImageTokens, usage.OutputTokenDetails.ImageTokens) &&
		(!usedVars["cr"] || addRealtimeToken(&total.InputTokenDetails.CachedTokens, usage.InputTokenDetails.CachedTokens)) &&
		addRealtimeToken(&total.InputTokenDetails.CachedCreationTokens, usage.InputTokenDetails.CacheCreationTokensTotal())
}

func addRealtimeToken(total *int, value int) bool {
	if (value > 0 && *total > math.MaxInt-value) || (value < 0 && *total < math.MinInt-value) {
		return false
	}
	*total += value
	return true
}

func mergeCachedTokenDetails(target *dto.InputTokenDetails, source *dto.CachedTokenDetails, usedVars map[string]bool) bool {
	if source == nil {
		return true
	}
	if target.CachedTokensDetails == nil {
		target.CachedTokensDetails = &dto.CachedTokenDetails{}
	}
	if usedVars["ai"] && source.AudioTokens != nil {
		value := *source.AudioTokens
		if target.CachedTokensDetails.AudioTokens != nil {
			if !addRealtimeToken(&value, *target.CachedTokensDetails.AudioTokens) {
				return false
			}
		}
		target.CachedTokensDetails.AudioTokens = &value
	}
	if usedVars["img"] && source.ImageTokens != nil {
		value := *source.ImageTokens
		if target.CachedTokensDetails.ImageTokens != nil {
			if !addRealtimeToken(&value, *target.CachedTokensDetails.ImageTokens) {
				return false
			}
		}
		target.CachedTokensDetails.ImageTokens = &value
	}
	return true
}
