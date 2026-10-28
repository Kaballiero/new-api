package test_channel

import "context"

type Input struct {
	Context         context.Context
	ID              int
	Model, Endpoint string
	Stream          bool
}
type ChannelTestResponse struct {
	Time      float64 `json:"time"`
	ErrorCode string  `json:"error_code,omitempty"`
}
type Output struct {
	Response      ChannelTestResponse
	Kind, Message string
}
