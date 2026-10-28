package poll_tasks

import (
	"context"
	"io"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Store preserves the existing inventory, bulk-failure and status-claim queries.
type Store interface {
	TimedOut(cutoffUnix int64, limit int) []*domain.Task
	UnfinishedPage(limit int, afterID int64) []*domain.Task
	BulkUpdate(ids []int64, fields map[string]any) error
	Transition(task *domain.Task, fromStatus domain.TaskStatus) (bool, error)
}

// Channel carries only polling transport configuration; BaseURL remains distinct
// from its provider default because adaptor initialization preserves that choice.
type Channel struct {
	Id                  int
	BaseURL             string
	FallbackBaseURL     string
	Key                 string
	Proxy               string
	DisablePollingSleep bool
}

type ChannelReader interface {
	Get(channelID int) (Channel, error)
}

// Response transfers body ownership to the poller, which closes it after parsing.
type Response struct {
	StatusCode int
	Headers    map[string][]string
	Body       io.ReadCloser
}

type Adaptor interface {
	Init(baseURL, key string)
	FetchTask(baseURL, key string, task *domain.Task, proxy string) (*Response, error)
	ParseTaskResult(task *domain.Task, response *Response, body []byte) (*domain.TaskInfo, error)
	AdjustBillingOnComplete(task *domain.Task, result *domain.TaskInfo) int
}

type BatchAdaptor interface {
	Adaptor
	FetchMode() string
	FetchBatchTasks(baseURL, key string, tasks []*domain.Task, proxy string) (*Response, error)
	ParseBatchResult(tasks []*domain.Task, response *Response, body []byte) (map[string]*BatchResult, error)
}

type BatchResult struct {
	TaskInfo   domain.TaskInfo
	SubmitTime int64
	StartTime  int64
	FinishTime int64
	Data       any
}

type Factory interface{ Open(platform string) Adaptor }

// Completion owns financial rules and effects; adjustment is deferred until the
// status claim wins, preserving provider hook order and the existing refund gate.
type Completion interface {
	Settle(ctx context.Context, task *domain.Task, result *domain.TaskInfo, adjust func(*domain.Task, *domain.TaskInfo) int) (bool, error)
	Prepare(task *domain.Task)
	Refund(ctx context.Context, task *domain.Task, reason string) bool
	IndependentRefund(task *domain.Task) bool
}

type Reconciler interface {
	Poll(ctx context.Context, task *domain.Task) error
}

// Codec owns stored/provider JSON and raw response redaction, without deciding
// task state or financial eligibility.
type Codec interface {
	Marshal(value any) ([]byte, error)
	Unmarshal(data []byte, value any) error
	RedactResponse(body []byte) []byte
	ParseGatewayResponse(body []byte) (result *domain.TaskInfo, data []byte, observation any, ok bool)
}

type Diagnostics interface {
	SysLog(message string)
	Info(ctx context.Context, message string)
	Warn(ctx context.Context, message string)
	Error(ctx context.Context, message string)
	Debug(ctx context.Context, format string, args ...any)
	Json(ctx context.Context, message string, value any)
}

type Limits struct {
	TimeoutMinutes func() int
	QueryLimit     func() int
	MaxFailures    func() int
}

type Texts interface {
	Timeout(userID, minutes int) string
	LegacyTimeout(userID int) string
}

type Usecase struct {
	texts       Texts
	store       Store
	channels    ChannelReader
	factory     Factory
	completion  Completion
	reconciler  Reconciler
	codec       Codec
	diagnostics Diagnostics
	limits      Limits
	now         func() time.Time
	spawn       func(func())
	proxyURL    func(taskID string) string
}

func NewUsecase(store Store, channels ChannelReader, factory Factory, completion Completion, reconciler Reconciler,
	codec Codec, diagnostics Diagnostics, limits Limits, now func() time.Time, spawn func(func()), proxyURL func(string) string, texts Texts) *Usecase {
	return &Usecase{texts: texts, store: store, channels: channels, factory: factory, completion: completion, reconciler: reconciler,
		codec: codec, diagnostics: diagnostics, limits: limits, now: now, spawn: spawn, proxyURL: proxyURL}
}
