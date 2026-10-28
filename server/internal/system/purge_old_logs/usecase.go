package purge_old_logs

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/system/domain"
)

type Tasks interface {
	Decode(string, any) error
	UpdateSystemTaskState(string, string, any) error
	FinishSystemTask(string, string, domain.SystemTaskStatus, any, string) error
}
type Logs interface {
	CountOldLog(context.Context, int64) (int64, error)
	DeleteOldLogBatch(context.Context, int64, int) (int64, error)
}
type Usecase struct {
	tasks     Tasks
	logs      Logs
	fail      func(*domain.SystemTask, string, error)
	lockError func(context.Context, *domain.SystemTask, error)
}

func NewUsecase(tasks Tasks, logs Logs, fail func(*domain.SystemTask, string, error), lockError func(context.Context, *domain.SystemTask, error)) Usecase {
	return Usecase{tasks: tasks, logs: logs, fail: fail, lockError: lockError}
}

type Input struct {
	Task     *domain.SystemTask
	RunnerID string
}

func (u Usecase) Execute(ctx context.Context, input Input) error {
	task, runnerID := input.Task, input.RunnerID
	payload := domain.LogCleanupPayload{}
	if err := u.tasks.Decode(task.Payload, &payload); err != nil {
		u.fail(task, runnerID, err)
		return err
	}
	if payload.TargetTimestamp <= 0 {
		err := errors.New("target timestamp is required")
		u.fail(task, runnerID, err)
		return err
	}
	if payload.BatchSize <= 0 {
		payload.BatchSize = 100
	}

	state := domain.LogCleanupState{}
	if err := u.tasks.Decode(task.State, &state); err != nil {
		u.fail(task, runnerID, err)
		return err
	}

	for {
		remaining, err := u.logs.CountOldLog(ctx, payload.TargetTimestamp)
		if err != nil {
			u.fail(task, runnerID, err)
			return err
		}
		syncLogCleanupStateFromRemaining(&state, remaining)
		if err := u.tasks.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
			u.lockError(ctx, task, err)
			return err
		}
		if state.Remaining == 0 {
			break
		}

		// Track whether this pass deleted anything so a fresh recount that still
		// reports remaining rows resumes immediately instead of waiting for the
		// lock to expire. If a whole pass deletes nothing while rows remain, the
		// rows cannot be removed and we fail instead of busy-looping.
		progressed := false
		for state.Remaining > 0 {
			rowsAffected, err := u.logs.DeleteOldLogBatch(ctx, payload.TargetTimestamp, payload.BatchSize)
			if err != nil {
				u.fail(task, runnerID, err)
				return err
			}
			if rowsAffected == 0 {
				break
			}
			progressed = true

			state.Processed += rowsAffected
			if state.Total < state.Processed {
				state.Total = state.Processed
			}
			if state.Remaining > rowsAffected {
				state.Remaining -= rowsAffected
			} else {
				state.Remaining = 0
			}
			state.Progress = logCleanupProgress(state.Processed, state.Total)

			if err := u.tasks.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
				u.lockError(ctx, task, err)
				return err
			}
		}

		if !progressed {
			err := errors.New("no log rows were deleted")
			u.fail(task, runnerID, err)
			return err
		}
	}

	state.Remaining = 0
	state.Progress = 100
	if state.Total < state.Processed {
		state.Total = state.Processed
	}
	if err := u.tasks.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		u.lockError(ctx, task, err)
		return err
	}

	result := domain.LogCleanupResult{DeletedCount: state.Processed}
	if err := u.tasks.FinishSystemTask(task.TaskID, runnerID, domain.SystemTaskStatusSucceeded, result, ""); err != nil {
		u.lockError(ctx, task, err)
		return err
	}

	return nil
}

func syncLogCleanupStateFromRemaining(state *domain.LogCleanupState, remaining int64) {
	if state.Total <= 0 {
		state.Total = remaining
		state.Processed = 0
	} else {
		processedFromRemaining := state.Total - remaining
		if processedFromRemaining > state.Processed {
			state.Processed = processedFromRemaining
		}
	}
	if state.Processed < 0 {
		state.Processed = 0
	}
	state.Remaining = remaining
	state.Progress = logCleanupProgress(state.Processed, state.Total)
}

func logCleanupProgress(processed int64, total int64) int {
	if total <= 0 {
		return 100
	}
	if processed <= 0 {
		return 0
	}
	if processed >= total {
		return 100
	}
	return int(processed * 100 / total)
}
