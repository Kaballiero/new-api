package observe_plugin_task

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	tasksdomain "github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type isolatedSession struct {
	Session
	id           string
	task         *tasksdomain.Task
	published    []string
	disconnected bool
	active       int
	errorActive  int
	submit       func(context.Context) (*tasksdomain.ProtocolSubmission, error)
}

func (s *isolatedSession) Acquire(string, int, int) (func(), error) {
	s.active++
	return func() { s.active-- }, nil
}
func (s *isolatedSession) Submit(ctx context.Context) (*tasksdomain.ProtocolSubmission, error) {
	return s.submit(ctx)
}
func (s *isolatedSession) RespondError(error)   { s.errorActive = s.active }
func (s *isolatedSession) Debug(string, ...any) {}
func (s *isolatedSession) Disconnected()        { s.disconnected = true }
func (s *isolatedSession) Durable(outcome *tasksdomain.ProtocolSubmission, background bool) error {
	s.task = outcome.Task
	return nil
}
func (s *isolatedSession) Pending(task *tasksdomain.Task) error {
	s.published = append(s.published, task.TaskID)
	return nil
}

// One assembled observer must never retain a request session, cancellation or durable outcome.
func TestObserverRequestIsolation(t *testing.T) {
	first := &isolatedSession{id: "task_first"}
	second := &isolatedSession{id: "task_second"}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	submit := func(current *isolatedSession) func(context.Context) (*tasksdomain.ProtocolSubmission, error) {
		return func(ctx context.Context) (*tasksdomain.ProtocolSubmission, error) {
			require.NoError(t, ctx.Err(), "submission lifetime is independent of the observer cancellation")
			return &tasksdomain.ProtocolSubmission{Task: &tasksdomain.Task{TaskID: current.id, UserId: 7, Platform: "example"}, OwnerID: 7}, nil
		}
	}
	first.submit, second.submit = submit(first), submit(second)
	observer := Usecase{}
	timing := domain.ObservationTiming{Submission: time.Minute}
	require.NoError(t, observer.Execute(cancelled, Input{Session: first, UserID: 7, TokenID: 1, PluginKey: "example", Background: true, Timing: timing}))
	require.NoError(t,
		observer.Execute(context.Background(),
			Input{Session: second,
				UserID:     7,
				TokenID:    1,
				PluginKey:  "example",
				Background: true,
				Timing:     timing}))
	assert.True(t, first.disconnected)
	assert.Nil(t, first.task)
	assert.Empty(t, first.published)
	assert.False(t, second.disconnected)
	require.NotNil(t, second.task)
	assert.Equal(t, "task_second", second.task.TaskID)
	assert.Equal(t, []string{"task_second"}, second.published)
	assert.Zero(t, first.active)
	assert.Zero(t, second.active)
}

// Gateway panics must release admission and cancel the bounded submission lifetime too.
func TestObserverPanicReleasesSubmission(t *testing.T) {
	session := &isolatedSession{id: "panic"}
	var submissionContext context.Context
	session.submit = func(ctx context.Context) (*tasksdomain.ProtocolSubmission, error) {
		submissionContext = ctx
		panic("gateway panic")
	}
	observer := Usecase{}
	require.Panics(t, func() {
		_ = observer.Execute(context.Background(), Input{Session: session, PluginKey: "example", UserID: 7, TokenID: 1, Timing: domain.ObservationTiming{Submission: time.Minute}})
	})
	require.NotNil(t, submissionContext)
	assert.ErrorIs(t, submissionContext.Err(), context.Canceled)
	assert.Zero(t, session.active)
}

func TestObserverRespondsBeforeReleasingAdmission(t *testing.T) {
	session := &isolatedSession{}
	session.submit = func(context.Context) (*tasksdomain.ProtocolSubmission, error) {
		return nil, domain.ErrProtocolSubmission
	}
	err := (Usecase{}).Execute(t.Context(), Input{Session: session, PluginKey: "example", UserID: 7, TokenID: 1, Timing: domain.ObservationTiming{Submission: time.Minute}})
	require.ErrorIs(t, err, domain.ErrProtocolSubmission)
	assert.Equal(t, 1, session.errorActive)
	assert.Zero(t, session.active)
}
