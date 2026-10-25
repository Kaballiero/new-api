package domain

import (
	"context"
	"encoding/json"
	"io"
)

type RequestItem struct {
	CustomID string          `json:"custom_id"`
	Body     json.RawMessage `json:"body"`
}

type Submission struct {
	ID       string
	Model    string
	Endpoint string
	Provider json.RawMessage
	Items    []RequestItem
	// OpenAI uploads its JSONL input before creating the paid batch. The host
	// must persist this ID before proceeding, so upload recovery is independent
	// of an ambiguous batch-create response.
	InputFileID string
}

type Request struct {
	Method string
	Path   string
	Body   []byte
}

type Snapshot struct {
	ID            string
	State         State
	NativeState   string
	OutputFileID  string
	ErrorFileID   string
	RequestCounts json.RawMessage
	Usage         json.RawMessage
	Error         json.RawMessage
}

type Result struct {
	CustomID   string          `json:"custom_id"`
	StatusCode int             `json:"status_code,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
	Error      json.RawMessage `json:"error,omitempty"`
	// Unbilled is set only when the native protocol explicitly guarantees it.
	// Missing results or usage never imply a zero charge.
	Unbilled bool `json:"unbilled"`
}

type Provider interface {
	Create(Submission) (Request, error)
	Poll(string) (Request, error)
	Cancel(string) (Request, error)
	Delete(string) (Request, error)
	ReadStatus(io.Reader, func(Result) error) (Snapshot, error)
	ReadResults(io.Reader, func(Result) error) error
}

type Response struct {
	StatusCode int
	RequestID  string
	Body       io.ReadCloser
}
type Gateway interface {
	ForChannel(int) (Provider, error)
	Request(context.Context, *Job, Request, io.Reader, string) (*Response, error)
	Upload(context.Context, *Job, Submission) (string, error)
	ResultsRequest(*Job, string) (Request, error)
}
type Decoder interface {
	Decode(any) error
	UseNumber()
}
type Codec interface {
	Marshal(any) ([]byte, error)
	Unmarshal([]byte, any) error
	UnmarshalJsonStr(string, any) error
	NewDecoder(io.Reader) Decoder
	Snapshot(string) (BillingSnapshot, error)
}
type ChannelKinds struct{ OpenAI, Anthropic, Google, OpenRouter int }
