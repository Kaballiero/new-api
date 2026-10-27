package tasks

import (
	"context"
	"github.com/QuantumNous/new-api/constant"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/confirm_submission_reservation"
)

// SubmissionPersistence is the durable insert capability.
// The gateway retains the original record so GORM hook mutations are observable
// before the billing settlement barrier, including on failed inserts.
type SubmissionPersistence interface {
	InsertTask(context.Context, *record.Task) error
}

// OriginReader preserves owned reads and the distinction between not-found and SQL failure.
type OriginReader interface {
	GetByTaskId(int, string) (*record.Task, bool, error)
}

// FetchClaims exposes only the existing guarded provider-fetch update.
type FetchClaims interface {
	UpdateTaskWithStatus(*record.Task, record.TaskStatus) (bool, error)
}
type ProtocolPersistence interface {
	GetTaskForProtocolObservation(context.Context, int, constant.TaskPlatform, string) (*record.Task, bool, error)
	MarkResponsesBackground(*record.Task) error
}

func Submissions() SubmissionPersistence { return NewStore() }
func Origins() OriginReader              { return NewStore() }
func FetchUpdates() FetchClaims          { return NewStore() }
func Protocols() ProtocolPersistence     { return NewStore() }

func SubmissionConfirmation(task *record.Task) confirm_submission_reservation.Usecase {
	return confirm_submission_reservation.NewUsecase(storage.SubmissionConfirmation{Store: NewStore(), Task: task})
}

// MidjourneyPersistence is the existing ingress read/write capability for the
// separate Midjourney record. It does not expose task polling or billing stores.
type MidjourneyPersistence interface {
	GetByOnlyMJId(string) *record.Midjourney
	GetByMJId(int, string) *record.Midjourney
	GetByMJIds(int, []string) []*record.Midjourney
	InsertMidjourney(*record.Midjourney) error
	UpdateMidjourney(*record.Midjourney) error
}

func MidjourneyIngress() MidjourneyPersistence { return NewStore() }
