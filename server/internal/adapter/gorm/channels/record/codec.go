package record

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

// Codec preserves channel field encodings; diagnostics are supplied by composition.
type Codec struct{ Log func(string) }

func (Codec) DecodeModelMapping(raw string) (map[string]string, error) {
	mapping := make(map[string]string)
	err := common.UnmarshalJsonStr(raw, &mapping)
	return mapping, err
}
func (Codec) EncodeModelMapping(mapping map[string]string) (string, error) {
	data, err := common.Marshal(mapping)
	return string(data), err
}
func (codec Codec) ReadOtherInfo(channel *domain.Channel) map[string]any {
	info := make(map[string]any)
	if channel.OtherInfo != "" {
		if err := common.Unmarshal([]byte(channel.OtherInfo), &info); err != nil {
			codec.Log(fmt.Sprintf("failed to unmarshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		}
	}
	return info
}
func (codec Codec) WriteOtherInfo(channel *domain.Channel, info map[string]any) {
	data, err := common.Marshal(info)
	if err != nil {
		codec.Log(fmt.Sprintf("failed to marshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		return
	}
	channel.OtherInfo = string(data)
}
func (Codec) DecodeVertexRegions(raw string) (map[string]any, error) { return common.StrToMap(raw) }
func (Codec) DecodeCodexFields(raw string) (map[string]any, error) {
	var fields map[string]any
	err := common.Unmarshal([]byte(raw), &fields)
	return fields, err
}
func (Codec) DecodeCodexKey(raw string) (*domain.CodexOAuthKey, error) {
	var key domain.CodexOAuthKey
	if err := common.Unmarshal([]byte(raw), &key); err != nil {
		return nil, err
	}
	return &key, nil
}
func (Codec) EncodeCodexKey(key *domain.CodexOAuthKey) (string, error) {
	data, err := common.Marshal(key)
	return string(data), err
}

// KeyArray preserves raw element bytes, unlike the Vertex account key encoding.
func (Codec) KeyArray(raw string) ([]string, error) {
	var array []json.RawMessage
	if err := common.Unmarshal([]byte(raw), &array); err != nil {
		return nil, err
	}
	keys := make([]string, len(array))
	for i, value := range array {
		keys[i] = string(value)
	}
	return keys, nil
}
func (Codec) DecodeVertexKeys(raw string) ([]string, error) {
	var array []any
	if err := common.Unmarshal([]byte(raw), &array); err != nil {
		return nil, &domain.DetailError{Err: domain.ErrVertexKeysInvalid, Detail: err.Error()}
	}
	keys := make([]string, 0, len(array))
	for _, value := range array {
		if text, ok := value.(string); ok {
			keys = append(keys, text)
			continue
		}
		data, err := common.Marshal(value)
		if err != nil {
			return nil, &domain.DetailError{Err: domain.ErrVertexKeyEncode, Detail: err.Error()}
		}
		keys = append(keys, string(data))
	}
	return keys, nil
}
func (Codec) DecodeHeaderOverride(raw string) (map[string]any, error) {
	var fields map[string]any
	err := common.UnmarshalJsonStr(raw, &fields)
	return fields, err
}

// ReconciledKeys retains the array-to-newline fallback of stored multi-key edits.
func (codec Codec) ReconciledKeys(raw string) []string {
	keys := []string{}
	if raw != "" {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "[") {
			if array, err := codec.KeyArray(trimmed); err == nil {
				keys = array
			}
		}
		if len(keys) == 0 {
			keys = strings.Split(strings.Trim(raw, "\n"), "\n")
		}
	}
	return keys
}
