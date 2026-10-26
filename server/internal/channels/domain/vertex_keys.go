package domain

import (
	"strings"
)

// VertexArrayKeys splits a JSON array of Vertex AI service account keys into
// one key per element.
func VertexArrayKeys(keys string, decode func(string) ([]string, error)) ([]string, error) {
	if keys == "" {
		return nil, nil
	}
	keyArray, err := decode(keys)
	if err != nil {
		return nil, err
	}
	cleanKeys := make([]string, 0, len(keyArray))
	for _, key := range keyArray {
		keyStr := strings.TrimSpace(key)
		if keyStr != "" {
			cleanKeys = append(cleanKeys, keyStr)
		}
	}
	if len(cleanKeys) == 0 {
		return nil, ErrVertexKeysEmpty
	}
	return cleanKeys, nil
}
