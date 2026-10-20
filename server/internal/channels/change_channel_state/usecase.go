package change_channel_state

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	LockStatus() func()
	LockPolling(int) func()
	CachedState(int) (*domain.Channel, error)
	ReadState(int) (*domain.Channel, error)
	StateKeys(*domain.Channel) []string
	CommitState(*domain.Channel)
	SetCacheStatus(int, int)
	SaveState(*domain.Channel) error
	SetAbilityStatus(int, bool) error
}
type Usecase struct {
	store  Store
	memory func() bool
	now    func() int64
	log    func(string)
}

func NewUsecase(store Store, memory func() bool, now func() int64, log func(string)) Usecase {
	return Usecase{store: store, memory: memory, now: now, log: log}
}
func (usecase Usecase) Execute(channelId int, usingKey string, status int, reason string) bool {
	if usecase.memory() {
		unlock := usecase.store.LockStatus()
		defer unlock()
	}

	// ChannelInfo stores both multi-key status and the polling cursor. Hold the
	// same per-channel lock from the first read through persistence so neither
	// writer can save a stale JSON snapshot over the other.
	unlock := usecase.store.LockPolling(channelId)
	defer unlock()

	if usecase.memory() {
		channelCache, _ := usecase.store.CachedState(channelId)
		if channelCache == nil {
			return false
		}
		if channelCache.ChannelInfo.IsMultiKey {
			beforeStatus := channelCache.Status
			// 如果是多Key模式，更新缓存中的状态
			domain.ApplyMultiKeyState(channelCache, usecase.store.StateKeys(channelCache), usingKey, status, reason, usecase.now, usecase.log)
			usecase.store.CommitState(channelCache)
			if beforeStatus != channelCache.Status {
				usecase.store.SetCacheStatus(channelId, channelCache.Status)
			}
			//CacheUpdateChannel(channelCache)
			//return true
		} else {
			// 如果缓存渠道存在，且状态已是目标状态，直接返回
			if channelCache.Status == status {
				return false
			}
			usecase.store.SetCacheStatus(channelId, status)
		}
	}

	shouldUpdateAbilities := false
	defer func() {
		if shouldUpdateAbilities {
			err := usecase.store.SetAbilityStatus(channelId, status == common.ChannelStatusEnabled)
			if err != nil {
				usecase.log(fmt.Sprintf("failed to update ability status: channel_id=%d, error=%v", channelId, err))
			}
		}
	}()
	channel, err := usecase.store.ReadState(channelId)
	if err != nil {
		return false
	} else {
		if channel.Status == status {
			return false
		}

		if channel.ChannelInfo.IsMultiKey {
			beforeStatus := channel.Status
			domain.ApplyMultiKeyState(channel, usecase.store.StateKeys(channel), usingKey, status, reason, usecase.now, usecase.log)
			if beforeStatus != channel.Status {
				shouldUpdateAbilities = true
			}
		} else {
			info := channel.GetOtherInfo()
			info["status_reason"] = reason
			info["status_time"] = usecase.now()
			channel.SetOtherInfo(info)
			channel.Status = status
			shouldUpdateAbilities = true
		}
		err = usecase.store.SaveState(channel)
		if err != nil {
			usecase.log(fmt.Sprintf("failed to update channel status: channel_id=%d, status=%d, error=%v", channel.Id, status, err))
			return false
		}
	}
	return true
}
