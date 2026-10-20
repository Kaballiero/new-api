package manage_multi_keys

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"strings"
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
func (usecase Usecase) Execute(request MultiKeyManageRequest) (Output, *domain.Failure) {

	channel, err := usecase.store.Get(request.ChannelId, true)
	if err != nil {
		return Output{
			Success: false,
			Message: "渠道不存在"}, nil
	}

	if !channel.ChannelInfo.IsMultiKey {
		return Output{
			Success: false,
			Message: "该渠道不是多密钥模式"}, nil
	}
	if requiresSensitiveWrite(request.Action) &&
		!usecase.canWriteSensitive() {
		return Output{}, &domain.Failure{Kind: "permission"}
	}

	// get_key_status 为只读查询，不记录审计；其余为修改操作，记录审计并跳过中间件兜底。
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
			Success: true,
			Message: "",
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
			return Output{
				Success: false,
				Message: "未指定要禁用的密钥索引"}, nil
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			return Output{
				Success: false,
				Message: "密钥索引超出范围"}, nil
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
			return Output{}, &domain.Failure{Kind: "internal", Cause: err}
		}

		usecase.refresh()
		return Output{
			Success: true,
			Message: "密钥已禁用"}, nil

	case "enable_key":
		if request.KeyIndex == nil {
			return Output{
				Success: false,
				Message: "未指定要启用的密钥索引"}, nil
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			return Output{
				Success: false,
				Message: "密钥索引超出范围"}, nil
		}

		// 从状态列表中删除该密钥的记录，使其回到默认启用状态
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
			return Output{}, &domain.Failure{Kind: "internal", Cause: err}
		}

		usecase.refresh()
		return Output{
			Success: true,
			Message: "密钥已启用"}, nil

	case "enable_all_keys":
		// 清空所有禁用状态，使所有密钥回到默认启用状态
		var enabledCount int
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			enabledCount = len(channel.ChannelInfo.MultiKeyStatusList)
		}

		channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)

		err = usecase.store.Update(channel)
		if err != nil {
			return Output{}, &domain.Failure{Kind: "internal", Cause: err}
		}

		usecase.refresh()
		return Output{
			Success: true,
			Message: fmt.Sprintf("已启用 %d 个密钥", enabledCount)}, nil

	case "disable_all_keys":
		// 禁用所有启用的密钥
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

			// 只禁用当前启用的密钥
			if status == 1 {
				channel.ChannelInfo.MultiKeyStatusList[i] = 2 // disabled
				disabledCount++
			}
		}

		if disabledCount == 0 {
			return Output{
				Success: false,
				Message: "没有可禁用的密钥"}, nil
		}

		err = usecase.store.Update(channel)
		if err != nil {
			return Output{}, &domain.Failure{Kind: "internal", Cause: err}
		}

		usecase.refresh()
		return Output{
			Success: true,
			Message: fmt.Sprintf("已禁用 %d 个密钥", disabledCount)}, nil

	case "delete_key":
		if request.KeyIndex == nil {
			return Output{
				Success: false,
				Message: "未指定要删除的密钥索引"}, nil
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			return Output{
				Success: false,
				Message: "密钥索引超出范围"}, nil
		}

		keys := usecase.store.Keys(channel)
		var remainingKeys []string
		var newStatusList = make(map[int]int)
		var newDisabledTime = make(map[int]int64)
		var newDisabledReason = make(map[int]string)

		newIndex := 0
		for i, key := range keys {
			// 跳过要删除的密钥
			if i == keyIndex {
				continue
			}

			remainingKeys = append(remainingKeys, key)

			// 保留其他密钥的状态信息，重新索引
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
			return Output{
				Success: false,
				Message: "不能删除最后一个密钥"}, nil
		}

		// Update channel with remaining keys
		channel.Key = strings.Join(remainingKeys, "\n")
		channel.ChannelInfo.MultiKeySize = len(remainingKeys)
		channel.ChannelInfo.MultiKeyStatusList = newStatusList
		channel.ChannelInfo.MultiKeyDisabledTime = newDisabledTime
		channel.ChannelInfo.MultiKeyDisabledReason = newDisabledReason

		err = usecase.store.Update(channel)
		if err != nil {
			return Output{}, &domain.Failure{Kind: "internal", Cause: err}
		}

		usecase.refresh()
		return Output{
			Success: true,
			Message: "密钥已删除"}, nil

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

			// 只删除自动禁用（status == 3）的密钥，保留启用（status == 1）和手动禁用（status == 2）的密钥
			if status == 3 {
				deletedCount++
			} else {
				remainingKeys = append(remainingKeys, key)
				// 保留非自动禁用密钥的状态信息，重新索引
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
			return Output{
				Success: false,
				Message: "没有需要删除的自动禁用密钥"}, nil
		}

		// Update channel with remaining keys
		channel.Key = strings.Join(remainingKeys, "\n")
		channel.ChannelInfo.MultiKeySize = len(remainingKeys)
		channel.ChannelInfo.MultiKeyStatusList = newStatusList
		channel.ChannelInfo.MultiKeyDisabledTime = newDisabledTime
		channel.ChannelInfo.MultiKeyDisabledReason = newDisabledReason

		err = usecase.store.Update(channel)
		if err != nil {
			return Output{}, &domain.Failure{Kind: "internal", Cause: err}
		}

		usecase.refresh()
		return Output{
			Success: true,
			Message: fmt.Sprintf("已删除 %d 个自动禁用的密钥", deletedCount),
			Data:    deletedCount}, nil

	default:
		return Output{
			Success: false,
			Message: "不支持的操作"}, nil
	}

}
func requiresSensitiveWrite(action string) bool {
	return action == "delete_key" || action == "delete_disabled_keys"
}
