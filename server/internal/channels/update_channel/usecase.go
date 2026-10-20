package update_channel

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"strings"
)

type Store interface {
	domain.Configuration
	IsMissing(error) bool
	WithLocked(int, func(*domain.Channel, func(*domain.Channel) error) error) error
	Canonicalize(*domain.Channel) error
	OtherSettings(*domain.Channel) dto.ChannelOtherSettings
}
type Usecase struct {
	store                            Store
	canBindPlugin, canWriteSensitive func() bool
	normalizeProxy                   func(string) (string, error)
	refresh                          func()
	invalidate                       func(string)
	audit                            func(map[string]any)
}

func NewUsecase(store Store, canBindPlugin, canWriteSensitive func() bool, normalizeProxy func(string) (string, error), refresh func(), invalidate func(string), audit func(map[string]any)) Usecase {
	return Usecase{store: store, canBindPlugin: canBindPlugin, canWriteSensitive: canWriteSensitive, normalizeProxy: normalizeProxy, refresh: refresh, invalidate: invalidate, audit: audit}
}
func (usecase Usecase) Execute(input Input) (PatchChannel, *domain.Failure) {
	requestChannel, requestData, rawBody := input.Request, input.Fields, input.Raw

	if _, ok := requestData["status"]; ok {
		return PatchChannel{}, &domain.Failure{Kind: "invalid"}
	}
	baseURLFromPluginDefault := requestChannel.Type == constant.ChannelTypeTaskPlugin &&
		(requestChannel.BaseURL == nil || strings.TrimSpace(*requestChannel.BaseURL) == "")

	var channel PatchChannel
	var originChannel *domain.Channel
	var validationErr error
	permissionErr := errors.New("channel sensitive write permission denied")
	pluginBindErr := errors.New("task plugin channels require the task_plugin.bind permission")
	err := usecase.store.WithLocked(requestChannel.Id, func(lockedChannel *domain.Channel, persist func(*domain.Channel) error) error {
		originChannel = lockedChannel

		lockedChannelData, err := common.Marshal(lockedChannel)
		if err != nil {
			return err
		}
		if err := common.Unmarshal(lockedChannelData, &channel.Channel); err != nil {
			return err
		}
		storedChannelInfo := channel.ChannelInfo
		channel.ChannelInfo = domain.ChannelInfo{}
		if err := common.Unmarshal(rawBody, &channel); err != nil {
			return err
		}
		channel.ChannelInfo = storedChannelInfo
		ClearReadOnlyFields(&channel, requestData)
		if channel.Type == 0 {
			channel.Type = lockedChannel.Type
		}
		if channel.Models == "" {
			channel.Models = lockedChannel.Models
		}
		if mappingValue, present := requestData["model_mapping"]; !present || mappingValue == nil {
			channel.ModelMapping = lockedChannel.ModelMapping
		}

		if channel.Type == constant.ChannelTypeTaskPlugin && !usecase.canBindPlugin() {
			return pluginBindErr
		}

		if err := usecase.store.Canonicalize(&channel.Channel); err != nil {
			validationErr = err
			return err
		}
		if err := domain.ValidateChannel(&channel.Channel, false, usecase.store); err != nil {
			validationErr = err
			return err
		}
		if HasSensitiveChanges(&channel, lockedChannel, requestData) &&
			!usecase.canWriteSensitive() {
			return permissionErr
		}

		if channel.MultiKeyMode != nil && *channel.MultiKeyMode != "" {
			channel.ChannelInfo.MultiKeyMode = constant.MultiKeyMode(*channel.MultiKeyMode)
		}

		if channel.KeyMode != nil && channel.ChannelInfo.IsMultiKey {
			switch *channel.KeyMode {
			case "append":
				if lockedChannel.Key != "" {
					var newKeys []string
					var existingKeys []string

					if strings.HasPrefix(strings.TrimSpace(lockedChannel.Key), "[") {
						var arr []json.RawMessage
						if err := common.Unmarshal([]byte(strings.TrimSpace(lockedChannel.Key)), &arr); err == nil {
							existingKeys = make([]string, len(arr))
							for i, v := range arr {
								existingKeys[i] = string(v)
							}
						}
					} else {
						existingKeys = strings.Split(strings.Trim(lockedChannel.Key, "\n"), "\n")
					}

					if channel.Type == constant.ChannelTypeVertexAi && usecase.store.OtherSettings(&channel.Channel).VertexKeyType != dto.VertexKeyTypeAPIKey {
						if strings.HasPrefix(strings.TrimSpace(channel.Key), "[") {
							array, err := domain.VertexArrayKeys(channel.Key)
							if err != nil {
								validationErr = fmt.Errorf("追加密钥解析失败: %w", err)
								return validationErr
							}
							newKeys = array
						} else {
							newKeys = []string{channel.Key}
						}
					} else {
						inputKeys := strings.Split(channel.Key, "\n")
						for _, key := range inputKeys {
							key = strings.TrimSpace(key)
							if key != "" {
								newKeys = append(newKeys, key)
							}
						}
					}

					seen := make(map[string]struct{}, len(existingKeys)+len(newKeys))
					for _, key := range existingKeys {
						normalized := strings.TrimSpace(key)
						if normalized != "" {
							seen[normalized] = struct{}{}
						}
					}
					dedupedNewKeys := make([]string, 0, len(newKeys))
					for _, key := range newKeys {
						normalized := strings.TrimSpace(key)
						if normalized == "" {
							continue
						}
						if _, ok := seen[normalized]; ok {
							continue
						}
						seen[normalized] = struct{}{}
						dedupedNewKeys = append(dedupedNewKeys, normalized)
					}

					allKeys := append(existingKeys, dedupedNewKeys...)
					channel.Key = strings.Join(allKeys, "\n")
				}
			case "replace":
			}
		}

		return persist(&channel.Channel)
	})
	if usecase.store.IsMissing(err) {
		return PatchChannel{}, &domain.Failure{Kind: "missing"}
	}
	if validationErr != nil {
		return PatchChannel{}, &domain.Failure{Kind: "validation", Message: validationErr.Error()}
	}
	if errors.Is(err, pluginBindErr) {
		return PatchChannel{}, &domain.Failure{Kind: "plugin", Message: pluginBindErr.Error()}
	}
	if errors.Is(err, permissionErr) {
		return PatchChannel{}, &domain.Failure{Kind: "permission"}
	}
	if err != nil {
		return PatchChannel{}, &domain.Failure{Kind: "internal", Cause: err}
	}
	originProxy := usecase.store.Settings(originChannel).Proxy
	proxyChanged := false
	if _, settingProvided := requestData["setting"]; settingProvided {
		newProxy, _ := usecase.normalizeProxy(usecase.store.Settings(&channel.Channel).Proxy)
		normalizedOriginProxy, originProxyErr := usecase.normalizeProxy(originProxy)
		proxyChanged = originProxyErr != nil || normalizedOriginProxy != newProxy
	}
	usecase.refresh()
	if proxyChanged {
		usecase.invalidate(originProxy)
	}
	// 记录变更的字段名（语言无关的字段标识），密钥仅记录"已更换"绝不记录内容。
	changedFields := make([]string, 0)
	if channel.Models != originChannel.Models {
		changedFields = append(changedFields, "models")
	}
	if channel.Group != originChannel.Group {
		changedFields = append(changedFields, "group")
	}
	if channel.Type != originChannel.Type {
		changedFields = append(changedFields, "type")
	}
	if !equalStringPtr(channel.BaseURL, originChannel.BaseURL) {
		changedFields = append(changedFields, "base_url")
	}
	if channel.Key != "" && channel.Key != originChannel.Key {
		changedFields = append(changedFields, "key")
	}
	updateAudit := map[string]any{
		"id":             channel.Id,
		"name":           channel.Name,
		"changed_fields": changedFields,
	}
	if baseURLFromPluginDefault {
		updateAudit["base_url_source"] = "plugin_default"
	}
	usecase.audit(updateAudit)
	channel.Key = ""
	channel.HideMultiKeyFailures()
	return channel, nil

}
func equalStringPtr(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
func HasSensitiveChanges(channel *PatchChannel, origin *domain.Channel, requestData map[string]any) bool {
	if _, ok := requestData["type"]; ok && channel.Type != origin.Type {
		return true
	}
	if _, ok := requestData["key"]; ok && channel.Key != "" && channel.Key != origin.Key {
		return true
	}
	if _, ok := requestData["base_url"]; ok && !equalStringPtr(channel.BaseURL, origin.BaseURL) {
		return true
	}
	if _, ok := requestData["openai_organization"]; ok && !equalStringPtr(channel.OpenAIOrganization, origin.OpenAIOrganization) {
		return true
	}
	if _, ok := requestData["header_override"]; ok && !equalStringPtr(channel.HeaderOverride, origin.HeaderOverride) {
		return true
	}
	if _, ok := requestData["param_override"]; ok && !equalStringPtr(channel.ParamOverride, origin.ParamOverride) {
		return true
	}
	if _, ok := requestData["setting"]; ok && !equalStringPtr(channel.Setting, origin.Setting) {
		return true
	}
	if _, ok := requestData["other"]; ok && channel.Other != origin.Other {
		return true
	}
	if _, ok := requestData["settings"]; ok && channel.OtherSettings != origin.OtherSettings {
		return true
	}
	if _, ok := requestData["key_mode"]; ok && channel.KeyMode != nil {
		return true
	}
	// Fail closed: any field present in the request that is neither a known
	// sensitive field (gated above) nor an explicitly classified non-sensitive
	// field must be treated as sensitive. This keeps a newly added channel field
	// from silently becoming editable by ChannelWrite-only admins until it is
	// consciously classified in NonSensitiveFields.
	for field := range requestData {
		if _, ok := SensitiveFields[field]; ok {
			continue
		}
		if _, ok := NonSensitiveFields[field]; ok {
			continue
		}
		if _, ok := OperationalFields[field]; ok {
			continue
		}
		if _, ok := ReadOnlyFields[field]; ok {
			continue
		}
		return true
	}
	return false
}

// SensitiveFields lists the channel fields whose modification requires
// ChannelSensitiveWrite. They are each checked individually in
// HasSensitiveChanges with a precise old-vs-new comparison; this set is
// used to exclude them from the fail-closed scan for unknown fields.
var SensitiveFields = map[string]struct{}{
	"type":                {},
	"key":                 {},
	"base_url":            {},
	"openai_organization": {},
	"header_override":     {},
	"param_override":      {},
	"setting":             {},
	"other":               {},
	"settings":            {},
	"key_mode":            {},
}

// OperationalFields lists fields managed by operation endpoints instead
// of the general channel edit endpoint.
var OperationalFields = map[string]struct{}{
	"status": {},
}

// ReadOnlyFields lists server-managed/accounting fields that the general
// channel edit endpoint must ignore even if a client sends them.
var ReadOnlyFields = map[string]struct{}{
	"created_time":         {},
	"test_time":            {},
	"response_time":        {},
	"balance":              {},
	"balance_updated_time": {},
	"used_quota":           {},
}

func ClearReadOnlyFields(channel *PatchChannel, requestData map[string]any) {
	if _, ok := requestData["created_time"]; ok {
		channel.CreatedTime = 0
	}
	if _, ok := requestData["test_time"]; ok {
		channel.TestTime = 0
	}
	if _, ok := requestData["response_time"]; ok {
		channel.ResponseTime = 0
	}
	if _, ok := requestData["balance"]; ok {
		channel.Balance = 0
	}
	if _, ok := requestData["balance_updated_time"]; ok {
		channel.BalanceUpdatedTime = 0
	}
	if _, ok := requestData["used_quota"]; ok {
		channel.UsedQuota = 0
	}
}

// NonSensitiveFields lists routing / server-managed channel
// fields a ChannelWrite admin may edit without ChannelSensitiveWrite. When a new
// field is added to model.Channel it must be added to either this set or
// SensitiveFields or OperationalFields; otherwise it falls through
// to the fail-closed branch and is treated as sensitive. The
// TestChannelFieldsAreClassified guard test enforces this.
var NonSensitiveFields = map[string]struct{}{
	"id":                  {},
	"test_model":          {},
	"name":                {},
	"weight":              {},
	"models":              {},
	"group":               {},
	"model_mapping":       {},
	"status_code_mapping": {},
	"priority":            {},
	"auto_ban":            {},
	"other_info":          {},
	"tag":                 {},
	"remark":              {},
	"channel_info":        {},
	"multi_key_mode":      {},
}
