package update_channel

import "github.com/QuantumNous/new-api/internal/channels/domain"

type PatchChannel struct {
	domain.Channel
	MultiKeyMode *string `json:"multi_key_mode"`
	KeyMode      *string `json:"key_mode"` // replace or append keys in multi-key mode
}

type Input struct {
	Request PatchChannel
	Raw     []byte
	Fields  map[string]any
}
