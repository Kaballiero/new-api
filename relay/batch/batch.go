// Package batch implements native asynchronous provider protocols. It does not
// own credentials, task persistence, retries, or financial settlement.
package batch

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

const MaxResultBytes = 8 << 20

type State string

const (
	Validating State = "validating"
	Running    State = "in_progress"
	Finalizing State = "finalizing"
	Cancelling State = "cancelling"
	Completed  State = "completed"
	Failed     State = "failed"
	Expired    State = "expired"
	Cancelled  State = "cancelled"
)

func (s State) Terminal() bool {
	return s == Completed || s == Failed || s == Expired || s == Cancelled
}

type Item struct {
	CustomID string          `json:"custom_id"`
	Body     json.RawMessage `json:"body"`
}

type Submission struct {
	ID       string
	Model    string
	Endpoint string
	Items    []Item
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
	ID           string
	State        State
	NativeState  string
	OutputFileID string
	ErrorFileID  string
	Usage        json.RawMessage
	Error        json.RawMessage
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

type Adapter interface {
	Create(Submission) (Request, error)
	Poll(string) (Request, error)
	Cancel(string) (Request, error)
	Delete(string) (Request, error)
	ReadStatus(io.Reader, func(Result) error) (Snapshot, error)
	ReadResults(io.Reader, func(Result) error) error
}

var ErrUnsupported = errors.New("batch operation is not supported by this provider")

func ForChannel(channelType int) (Adapter, error) {
	switch channelType {
	case constant.ChannelTypeOpenAI:
		return OpenAI{}, nil
	case constant.ChannelTypeOpenRouter:
		return OpenRouter{}, nil
	case constant.ChannelTypeAnthropic:
		return Anthropic{}, nil
	case constant.ChannelTypeGemini:
		return Google{}, nil
	default:
		return nil, ErrUnsupported
	}
}

func Supports(channelType int, endpoint string) bool {
	switch channelType {
	case constant.ChannelTypeOpenAI:
		return endpoint == "/v1/chat/completions" || endpoint == "/v1/responses" || endpoint == "/v1/embeddings"
	case constant.ChannelTypeOpenRouter:
		return endpoint == "/v1/chat/completions" || endpoint == "/v1/responses" || endpoint == "/v1/embeddings" || endpoint == "/v1/messages"
	case constant.ChannelTypeAnthropic:
		return endpoint == "/v1/messages"
	case constant.ChannelTypeGemini:
		return endpoint == "/v1beta/generateContent"
	default:
		return false
	}
}

var nativeID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
var anthropicCustomID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// ResourceRequest accepts resource IDs rather than upstream URLs, preventing
// a returned results URL from redirecting credentials to a different host.
func resourceRequest(method, prefix, id, suffix string) (Request, error) {
	if !nativeID.MatchString(id) {
		return Request{}, errors.New("invalid batch resource ID")
	}
	return Request{Method: method, Path: prefix + id + suffix}, nil
}

func marshalRequest(path string, body any) (Request, error) {
	data, err := common.Marshal(body)
	return Request{Method: http.MethodPost, Path: path, Body: data}, err
}

// RequestBody preserves optional explicit zero/false values and unknown native
// fields. The public API validator owns model access and billing bounds.
func requestBody(item Item, model string, insertModel bool) (json.RawMessage, error) {
	if len(item.Body) == 0 || common.GetJsonType(item.Body) != "object" || common.HasDuplicateJSONKeys(item.Body) {
		return nil, fmt.Errorf("request %q must have an object body without duplicate keys", item.CustomID)
	}
	var body map[string]json.RawMessage
	if err := common.Unmarshal(item.Body, &body); err != nil {
		return nil, err
	}
	if raw, exists := body["stream"]; exists {
		var stream bool
		if err := common.Unmarshal(raw, &stream); err != nil || stream {
			return nil, errors.New("batch requests cannot stream")
		}
	}
	if raw, exists := body["model"]; exists {
		var supplied string
		if err := common.Unmarshal(raw, &supplied); err != nil || supplied != model {
			return nil, errors.New("batch request model must match the batch model")
		}
	}
	if insertModel {
		body["model"], _ = common.Marshal(model)
	}
	return common.Marshal(body)
}

func readJSONLines(reader io.Reader, decode func([]byte) (Result, error), emit func(Result) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), MaxResultBytes)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		result, err := decode(scanner.Bytes())
		if err != nil {
			return err
		}
		if result.CustomID == "" {
			return errors.New("batch result has no custom_id")
		}
		if err := emit(result); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// Each value is bounded independently, while the enclosing array may be much
// larger. A limited reader also bounds decoder read-ahead across item borders.
type resultStream struct {
	reader  *io.LimitedReader
	decoder *common.JSONStreamDecoder
}

func newResultStream(reader io.Reader) *resultStream {
	limited := &io.LimitedReader{R: reader, N: MaxResultBytes}
	return &resultStream{reader: limited, decoder: common.NewJSONStreamDecoder(limited)}
}

func (s *resultStream) token() (json.Token, error) {
	s.reader.N = MaxResultBytes
	before := s.decoder.InputOffset()
	token, err := s.decoder.Token()
	if s.decoder.InputOffset()-before > MaxResultBytes {
		return nil, errors.New("batch JSON token exceeds the result size limit")
	}
	return token, err
}

func (s *resultStream) decode(value any) error {
	s.reader.N = MaxResultBytes
	before := s.decoder.InputOffset()
	err := s.decoder.Decode(value)
	if s.decoder.InputOffset()-before > MaxResultBytes {
		return errors.New("batch result exceeds the result size limit")
	}
	return err
}

func (s *resultStream) object(field func(string) error) error {
	token, err := s.token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return errors.New("expected batch JSON object")
	}
	for s.decoder.More() {
		token, err := s.token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("invalid batch object key")
		}
		if err := field(key); err != nil {
			return err
		}
	}
	token, err = s.token()
	if err == nil && token != json.Delim('}') {
		return errors.New("invalid batch object ending")
	}
	return err
}

func (s *resultStream) array(consume func() error) error {
	token, err := s.token()
	if err != nil {
		return err
	}
	if token == nil {
		return nil
	}
	if token != json.Delim('[') {
		return errors.New("expected batch JSON array")
	}
	for s.decoder.More() {
		if err := consume(); err != nil {
			return err
		}
	}
	_, err = s.token()
	return err
}

func (s *resultStream) skip() error {
	// Discard unknown fields incrementally, including Google's echoed inputConfig.
	token, err := s.token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') && token != json.Delim('[') {
		return nil
	}
	for s.decoder.More() {
		if err := s.skip(); err != nil {
			return err
		}
	}
	_, err = s.token()
	return err
}

func (s *resultStream) end() error {
	_, err := s.token()
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return errors.New("unexpected data after batch JSON")
	}
	return err
}

func requireState(snapshot Snapshot, err error) (Snapshot, error) {
	if err != nil {
		return snapshot, err
	}
	if snapshot.ID == "" || snapshot.State == "" {
		return snapshot, errors.New("missing or unknown batch ID/state")
	}
	return snapshot, nil
}

func googleID(name string) (string, error) {
	id, ok := strings.CutPrefix(name, "batches/")
	if !ok || !nativeID.MatchString(id) {
		return "", errors.New("invalid Google batch name")
	}
	return id, nil
}
