package manage_multi_keys

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	Keys(*domain.Channel) []string
	Update(*domain.Channel) error
	LockPolling(int) func()
}
type Usecase struct {
	store             Store
	refresh           func()
	canWriteSensitive func() bool
	readAudit         func()
	writeAudit        func(string, int)
}

func NewUsecase(store Store, refresh func(), canWriteSensitive func() bool, readAudit func(), writeAudit func(string, int)) Usecase {
	return Usecase{store: store, refresh: refresh, canWriteSensitive: canWriteSensitive, readAudit: readAudit, writeAudit: writeAudit}
}

// Execute returns storage failures as they are: they are answered with their
// text and HTTP 200.
func (usecase Usecase) Execute(request MultiKeyManageRequest) (Output, error) {
	channel, err := usecase.store.Get(request.ChannelId, true)
	if err != nil {
		return Output{}, domain.ErrMultiKeyChannelNotFound
	}
	if !channel.ChannelInfo.IsMultiKey {
		return Output{}, domain.ErrNotMultiKey
	}
	if requiresSensitiveWrite(request.Action) &&
		!usecase.canWriteSensitive() {
		return Output{}, domain.ErrSensitiveWriteDenied
	}
	// get_key_status is a read-only query and is not audited; the other actions
	// change the channel, record their audit and skip the middleware fallback.
	if request.Action == "get_key_status" {
		usecase.readAudit()
	} else {
		usecase.writeAudit(request.Action, channel.Id)
	}
	unlock := usecase.store.LockPolling(channel.Id)
	defer unlock()
	switch request.Action {
	case "get_key_status":
		keys := usecase.store.Keys(channel)
		// Default pagination parameters
		page := request.Page
		pageSize := request.PageSize
		if page <= 0 {
			page = 1
		}
		if pageSize <= 0 {
			pageSize = 50 // Default page size
		}
		// Statistics for all keys (unchanged by filtering)
		var enabledCount, manualDisabledCount, autoDisabledCount int
		// Build all key status data first
		var allKeyStatusList []KeyStatus
		for i, key := range keys {
			status := 1 // default enabled
			var disabledTime int64
			var reason string
			if channel.ChannelInfo.MultiKeyStatusList != nil {
				if s, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
					status = s
				}
			}
			// Count for statistics (all keys)
			switch status {
			case 1:
				enabledCount++
			case 2:
				manualDisabledCount++
			case 3:
				autoDisabledCount++
			}
			if status != 1 {
				if channel.ChannelInfo.MultiKeyDisabledTime != nil {
					disabledTime = channel.ChannelInfo.MultiKeyDisabledTime[i]
				}
				if channel.ChannelInfo.MultiKeyDisabledReason != nil {
					reason = channel.ChannelInfo.MultiKeyDisabledReason[i]
				}
			}
			// Create key preview (first 10 chars)
			keyPreview := key
			if len(key) > 10 {
				keyPreview = key[:10] + "..."
			}
			allKeyStatusList = append(allKeyStatusList, KeyStatus{
				Index:        i,
				Status:       status,
				DisabledTime: disabledTime,
				Reason:       reason,
				KeyPreview:   keyPreview,
			})
		}
		// Apply status filter if specified
		var filteredKeyStatusList []KeyStatus
		if request.Status != nil {
			for _, keyStatus := range allKeyStatusList {
				if keyStatus.Status == *request.Status {
					filteredKeyStatusList = append(filteredKeyStatusList, keyStatus)
				}
			}
		} else {
			filteredKeyStatusList = allKeyStatusList
		}
		// Calculate pagination based on filtered results
		filteredTotal := len(filteredKeyStatusList)
		totalPages := (filteredTotal + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}
		// Calculate range for current page
		start := (page - 1) * pageSize
		end := start + pageSize
		if end > filteredTotal {
			end = filteredTotal
		}
		// Get the page data
		var pageKeyStatusList []KeyStatus
		if start < filteredTotal {
			pageKeyStatusList = filteredKeyStatusList[start:end]
		}
		return Output{
			Outcome: StatusListed,
			Data: MultiKeyStatusResponse{
				Keys:                pageKeyStatusList,
				Total:               filteredTotal, // Total of filtered results
				Page:                page,
				PageSize:            pageSize,
				TotalPages:          totalPages,
				EnabledCount:        enabledCount,        // Overall statistics
				ManualDisabledCount: manualDisabledCount, // Overall statistics
				AutoDisabledCount:   autoDisabledCount,   // Overall statistics
			}}, nil
	case "disable_key":
		if request.KeyIndex == nil {
			return Output{}, domain.ErrDisableKeyIndexRequired
		}
		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			return Output{}, domain.ErrKeyIndexOutOfRange
		}
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}
		channel.ChannelInfo.MultiKeyStatusList[keyIndex] = 2 // disabled
		err = usecase.store.Update(channel)
		if err != nil {
			return Output{}, err
		}
		usecase.refresh()
		return Output{Outcome: KeyDisabled}, nil
	case "enable_key":
		if request.KeyIndex == nil {
			return Output{}, domain.ErrEnableKeyIndexRequired
		}
		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			return Output{}, domain.ErrKeyIndexOutOfRange
		}
		// Drop the key from the status maps so that it is enabled by default again.
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime != nil {
			delete(channel.ChannelInfo.MultiKeyDisabledTime, keyIndex)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason != nil {
			delete(channel.ChannelInfo.MultiKeyDisabledReason, keyIndex)
		}
		err = usecase.store.Update(channel)
		if err != nil {
			return Output{}, err
		}
		usecase.refresh()
		return Output{Outcome: KeyEnabled}, nil
	case "enable_all_keys":
		// Clear every disabled status so that all keys are enabled by default.
		var enabledCount int
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			enabledCount = len(channel.ChannelInfo.MultiKeyStatusList)
		}
		channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		err = usecase.store.Update(channel)
		if err != nil {
			return Output{}, err
		}
		usecase.refresh()
		return Output{Outcome: KeysEnabled, Count: enabledCount}, nil
	case "disable_all_keys":
		// Disable every enabled key.
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}
		var disabledCount int
		for i := range channel.ChannelInfo.MultiKeySize {
			status := 1 // default enabled
			if s, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
				status = s
			}
			// Only currently enabled keys are disabled.
			if status == 1 {
				channel.ChannelInfo.MultiKeyStatusList[i] = 2 // disabled
				disabledCount++
			}
		}
		if disabledCount == 0 {
			return Output{}, domain.ErrNoKeysToDisable
		}
		err = usecase.store.Update(channel)
		if err != nil {
			return Output{}, err
		}
		usecase.refresh()
		return Output{Outcome: KeysDisabled, Count: disabledCount}, nil
	case "delete_key":
		if request.KeyIndex == nil {
			return Output{}, domain.ErrDeleteKeyIndexRequired
		}
		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			return Output{}, domain.ErrKeyIndexOutOfRange
		}
		keys := usecase.store.Keys(channel)
		var remainingKeys []string
		var newStatusList = make(map[int]int)
		var newDisabledTime = make(map[int]int64)
		var newDisabledReason = make(map[int]string)
		newIndex := 0
		for i, key := range keys {
			// Skip the key being deleted.
			if i == keyIndex {
				continue
			}
			remainingKeys = append(remainingKeys, key)
			// Keep the status of the other keys under their new indexes.
			if channel.ChannelInfo.MultiKeyStatusList != nil {
				if status, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists && status != 1 {
					newStatusList[newIndex] = status
				}
			}
			if channel.ChannelInfo.MultiKeyDisabledTime != nil {
				if t, exists := channel.ChannelInfo.MultiKeyDisabledTime[i]; exists {
					newDisabledTime[newIndex] = t
				}
			}
			if channel.ChannelInfo.MultiKeyDisabledReason != nil {
				if r, exists := channel.ChannelInfo.MultiKeyDisabledReason[i]; exists {
					newDisabledReason[newIndex] = r
				}
			}
			newIndex++
		}
		if len(remainingKeys) == 0 {
			return Output{}, domain.ErrLastKeyNotDeletable
		}
		// Update channel with remaining keys
		channel.Key = strings.Join(remainingKeys, "\n")
		channel.ChannelInfo.MultiKeySize = len(remainingKeys)
		channel.ChannelInfo.MultiKeyStatusList = newStatusList
		channel.ChannelInfo.MultiKeyDisabledTime = newDisabledTime
		channel.ChannelInfo.MultiKeyDisabledReason = newDisabledReason
		err = usecase.store.Update(channel)
		if err != nil {
			return Output{}, err
		}
		usecase.refresh()
		return Output{Outcome: KeyDeleted}, nil
	case "delete_disabled_keys":
		keys := usecase.store.Keys(channel)
		var remainingKeys []string
		var deletedCount int
		var newStatusList = make(map[int]int)
		var newDisabledTime = make(map[int]int64)
		var newDisabledReason = make(map[int]string)
		newIndex := 0
		for i, key := range keys {
			status := 1 // default enabled
			if channel.ChannelInfo.MultiKeyStatusList != nil {
				if s, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
					status = s
				}
			}
			// Delete only automatically disabled keys (status 3); enabled (1)
			// and manually disabled (2) keys stay.
			if status == 3 {
				deletedCount++
			} else {
				remainingKeys = append(remainingKeys, key)
				// Keep the status of the remaining keys under their new indexes.
				if status != 1 {
					newStatusList[newIndex] = status
					if channel.ChannelInfo.MultiKeyDisabledTime != nil {
						if t, exists := channel.ChannelInfo.MultiKeyDisabledTime[i]; exists {
							newDisabledTime[newIndex] = t
						}
					}
					if channel.ChannelInfo.MultiKeyDisabledReason != nil {
						if r, exists := channel.ChannelInfo.MultiKeyDisabledReason[i]; exists {
							newDisabledReason[newIndex] = r
						}
					}
				}
				newIndex++
			}
		}
		if deletedCount == 0 {
			return Output{}, domain.ErrNoAutoDisabledKeys
		}
		// Update channel with remaining keys
		channel.Key = strings.Join(remainingKeys, "\n")
		channel.ChannelInfo.MultiKeySize = len(remainingKeys)
		channel.ChannelInfo.MultiKeyStatusList = newStatusList
		channel.ChannelInfo.MultiKeyDisabledTime = newDisabledTime
		channel.ChannelInfo.MultiKeyDisabledReason = newDisabledReason
		err = usecase.store.Update(channel)
		if err != nil {
			return Output{}, err
		}
		usecase.refresh()
		return Output{Outcome: AutoDisabledKeysDeleted, Count: deletedCount, Data: deletedCount}, nil
	default:
		return Output{}, domain.ErrMultiKeyActionUnsupported
	}
}
func requiresSensitiveWrite(action string) bool {
	return action == "delete_key" || action == "delete_disabled_keys"
}
