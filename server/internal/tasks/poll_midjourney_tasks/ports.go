package poll_midjourney_tasks

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Store retains the existing unfinished inventory, bulk failures and status CAS.
type Store interface {
	Unfinished() []*domain.Midjourney
	BulkUpdateByIDs(ids []int, fields map[string]any) error
	BulkUpdateByTaskIDs(taskIDs []string, fields map[string]any) error
	Transition(task *domain.Midjourney, fromStatus string) (bool, error)
}

type Channel struct {
	BaseURL string
	Key     string
}
type ChannelReader interface {
	Get(channelID int) (Channel, error)
}

// Client owns the single grouped HTTP exchange, including its response lifecycle.
// Diagnostics for transport failures retain their historical order and bytes.
type Client interface {
	Fetch(ctx context.Context, channel Channel, taskIDs []string) ([]Result, error)
}

type Result struct {
	MjId       string
	Progress   string
	PromptEn   string
	State      string
	SubmitTime int64
	StartTime  int64
	FinishTime int64
	ImageUrl   string
	Status     string
	FailReason string
	Properties any
	Buttons    any
	VideoUrl   string
	VideoUrls  []VideoURL
}

type VideoURL struct {
	Url string `json:"url"`
}

type Codec interface {
	Marshal(value any) ([]byte, error)
}
type Refund interface {
	RefundMidjourney(ctx context.Context, task *domain.Midjourney, reason string) bool
}
type Diagnostics interface {
	Info(ctx context.Context, message string)
	Warn(ctx context.Context, message string)
	Error(ctx context.Context, message string)
}

type Texts interface {
	MidjourneyTimeout(userID int) string
	MidjourneyFailure(userID int) string
}

type Usecase struct {
	texts       Texts
	store       Store
	channels    ChannelReader
	client      Client
	codec       Codec
	refund      Refund
	diagnostics Diagnostics
	now         func() time.Time
}

func NewUsecase(store Store, channels ChannelReader, client Client, codec Codec, refund Refund, diagnostics Diagnostics,
	now func() time.Time, texts Texts) *Usecase {
	return &Usecase{texts: texts, store: store, channels: channels, client: client, codec: codec, refund: refund, diagnostics: diagnostics, now: now}
}
