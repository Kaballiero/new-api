package domain

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// VertexArrayKeys splits a JSON array of Vertex AI service account keys into
// one key per element.
func VertexArrayKeys(keys string) ([]string, error) {
	if keys == "" {
		return nil, nil
	}
	var keyArray []any
	err := common.Unmarshal([]byte(keys), &keyArray)
	if err != nil {
		return nil, &DetailError{Err: ErrVertexKeysInvalid, Detail: err.Error()}
	}
	cleanKeys := make([]string, 0, len(keyArray))
	for _, key := range keyArray {
		var keyStr string
		switch v := key.(type) {
		case string:
			keyStr = strings.TrimSpace(v)
		default:
			bytes, err := common.Marshal(v)
			if err != nil {
				return nil, &DetailError{Err: ErrVertexKeyEncode, Detail: err.Error()}
			}
			keyStr = string(bytes)
		}
		if keyStr != "" {
			cleanKeys = append(cleanKeys, keyStr)
		}
	}
	if len(cleanKeys) == 0 {
		return nil, ErrVertexKeysEmpty
	}
	return cleanKeys, nil
}
