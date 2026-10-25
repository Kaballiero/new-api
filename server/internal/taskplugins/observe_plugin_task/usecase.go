package observe_plugin_task

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	tasksdomain "github.com/QuantumNous/new-api/internal/tasks/domain"
)

type RenderResult struct {
	Done, Overloaded bool
	HookElapsed      time.Duration
}
type Session interface {
	Submit(context.Context) (*tasksdomain.ProtocolSubmission, error)
	RespondError(error)
	Acquire(string, int, int) (func(), error)
	Now() time.Time
	Debug(string, ...any)
	Warn(string)
	LogError(string)
	Disconnected()
	Durable(*tasksdomain.ProtocolSubmission, bool) error
	Pending(*tasksdomain.Task) error
	BeginStream() error
	LoadTask(context.Context, int, string, string) (*tasksdomain.Task, bool, error)
	RenderStream(context.Context, *tasksdomain.Task, uint64) (RenderResult, error)
	RenderFinal(context.Context, *tasksdomain.Task) (map[string]any, time.Duration, error)
	WriteFinal(map[string]any) error
	Timeout(bool, string)
	Failure(bool, string)
	WaitTick(context.Context, *time.Ticker, time.Duration) bool
}
type Usecase struct{}

type Input struct {
	Session            Session
	UserID, TokenID    int
	PluginKey          string
	Generation         uint64
	Stream, Background bool
	Timing             domain.ObservationTiming
}

func (u Usecase) Execute(ctx context.Context, input Input) (err error) {
	session := input.Session
	release, err := session.Acquire(input.PluginKey, input.UserID, input.TokenID)
	if err != nil {
		session.RespondError(err)
		return err
	}
	defer release()
	defer func() {
		if err != nil {
			session.RespondError(err)
		}
	}()
	session.Debug("task_plugin subsystem=protocol event=admission_acquired generation=%d plugin=%q", input.Generation, input.PluginKey)
	submissionContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), input.Timing.Submission)
	defer cancel()
	outcome, err := session.Submit(submissionContext)
	cancel()
	if ctx.Err() != nil {
		session.Disconnected()
		return nil
	}
	if err != nil {
		return err
	}
	if outcome == nil || outcome.Task == nil || outcome.Task.UserId != outcome.OwnerID || outcome.Task.Platform != input.PluginKey {
		return domain.ErrInvalidDurableOutcome
	}
	if err = session.Durable(outcome, input.Background); err != nil {
		return err
	}
	if input.Background && !input.Stream {
		return session.Pending(outcome.Task)
	}
	mode := "nonstream"
	if input.Stream {
		mode = "stream"
	}
	session.Debug("task_plugin subsystem=protocol event=observation_enter generation=%d plugin=%q mode=%s public_task_id=%q",
		input.Generation,
		input.PluginKey,
		mode,
		outcome.Task.TaskID)
	if input.Stream {
		return stream(ctx, input, outcome.Task.TaskID)
	}
	return wait(ctx, input, outcome.Task.TaskID)
}

func stream(ctx context.Context, input Input, taskID string) error {
	session := input.Session
	generation := input.Generation
	pluginKey := input.PluginKey
	session.Debug(
		"task_plugin subsystem=protocol event=observation_start generation=%d plugin=%q "+
			"mode=stream public_task_id=%q timeout_ms=%d tick_ms=%d heartbeat_ms=%d",
		generation,
		pluginKey,
		taskID,
		input.Timing.Observation.Milliseconds(),
		input.Timing.Tick.Milliseconds(),
		input.Timing.Heartbeat.Milliseconds(),
	)
	if err := session.BeginStream(); err != nil {
		return err
	}

	observationContext, cancelObservation := context.WithTimeout(ctx, input.Timing.Observation)
	defer cancelObservation()
	heartbeatTicker := time.NewTicker(input.Timing.Heartbeat)
	defer heartbeatTicker.Stop()

	tickNumber := uint64(0)
	lastStatus := ""
	for {
		loadStarted := session.Now()
		loadContext, cancelLoad := context.WithTimeout(observationContext, input.Timing.Load)
		task, exists, loadErr := session.LoadTask(
			loadContext,
			input.UserID,
			input.PluginKey,
			taskID,
		)
		loadContextErr := loadContext.Err()
		cancelLoad()
		loadElapsed := session.Now().Sub(loadStarted)
		if errors.Is(loadContextErr, context.DeadlineExceeded) &&
			observationContext.Err() == nil &&
			ctx.Err() == nil {
			session.Warn(fmt.Sprintf(
				"task protocol database observation overloaded; plugin=%s task=%s",
				input.PluginKey,
				taskID,
			))
			session.Debug(
				"task_plugin subsystem=protocol event=observation_tick generation=%d plugin=%q mode=stream tick=%d load_ms=%d overloaded=true",
				generation,
				pluginKey,
				tickNumber,
				loadElapsed.Milliseconds(),
			)
			delay := tickDelay(taskID, tickNumber, input.Timing.Tick, input.Timing.Jitter) + input.Timing.Tick
			tickNumber++
			if !session.WaitTick(observationContext, heartbeatTicker, delay) {
				if errors.Is(observationContext.Err(), context.DeadlineExceeded) {
					session.Debug("task_plugin subsystem=protocol event=observation_timeout generation=%d plugin=%q "+
						"mode=stream last_status=%q",
						generation,
						pluginKey,
						domain.DebugTaskStatus(lastStatus))
					session.Timeout(true, lastStatus)
				} else if ctx.Err() != nil {
					session.Debug("task_plugin subsystem=protocol event=client_disconnected generation=%d plugin=%q "+
						"mode=stream stage=backoff_wait",
						generation,
						pluginKey)
				} else {
					session.Debug("task_plugin subsystem=protocol event=client_write_failed generation=%d plugin=%q "+
						"mode=stream stage=heartbeat",
						generation,
						pluginKey)
				}
				return nil
			}
			continue
		}
		if loadErr != nil || !exists || task == nil {
			if errors.Is(observationContext.Err(), context.DeadlineExceeded) {
				session.Debug("task_plugin subsystem=protocol event=observation_timeout generation=%d plugin=%q "+
					"mode=stream last_status=%q",
					generation,
					pluginKey,
					domain.DebugTaskStatus(lastStatus))
				session.Timeout(true, lastStatus)
				return nil
			}
			if loadErr != nil && !errors.Is(loadErr, context.Canceled) {
				session.LogError("task protocol database observation failed")
			}
			if ctx.Err() == nil {
				session.Debug("task_plugin subsystem=protocol event=observation_failed generation=%d plugin=%q "+
					"mode=stream stage=load reason=task_unavailable",
					generation,
					pluginKey)
				session.Failure(true, lastStatus)
			} else {
				session.Debug("task_plugin subsystem=protocol event=client_disconnected generation=%d plugin=%q mode=stream stage=load", generation, pluginKey)
			}
			return nil
		}
		previousStatus := lastStatus
		lastStatus = string(task.Status)
		if lastStatus != previousStatus {
			session.Debug(
				"task_plugin subsystem=protocol event=status_transition generation=%d plugin=%q mode=stream tick=%d previous=%q status=%q load_ms=%d",
				generation,
				pluginKey,
				tickNumber,
				domain.DebugTaskStatus(previousStatus),
				domain.DebugTaskStatus(lastStatus),
				loadElapsed.Milliseconds(),
			)
		}
		rendered, renderErr := session.RenderStream(observationContext, task, tickNumber)
		if renderErr != nil {
			return renderErr
		}
		if rendered.Done {
			return nil
		}
		hookElapsed, overloaded := rendered.HookElapsed, rendered.Overloaded

		delay := tickDelay(taskID, tickNumber, input.Timing.Tick, input.Timing.Jitter)
		tickNumber++
		if overloaded {
			delay += input.Timing.Tick
		} else if hookElapsed > input.Timing.Tick {
			delay += input.Timing.Tick
			session.Warn(fmt.Sprintf(
				"task protocol render hook slow; plugin=%s task=%s elapsed_ms=%d",
				input.PluginKey,
				taskID,
				hookElapsed.Milliseconds(),
			))
		}
		if !session.WaitTick(observationContext, heartbeatTicker, delay) {
			if errors.Is(observationContext.Err(), context.DeadlineExceeded) {
				session.Debug("task_plugin subsystem=protocol event=observation_timeout generation=%d plugin=%q "+
					"mode=stream last_status=%q",
					generation,
					pluginKey,
					domain.DebugTaskStatus(lastStatus))
				session.Timeout(true, lastStatus)
			} else if ctx.Err() != nil {
				session.Debug("task_plugin subsystem=protocol event=client_disconnected generation=%d plugin=%q "+
					"mode=stream stage=tick_wait",
					generation,
					pluginKey)
			} else {
				session.Debug("task_plugin subsystem=protocol event=client_write_failed generation=%d plugin=%q "+
					"mode=stream stage=heartbeat",
					generation,
					pluginKey)
			}
			return nil
		}
	}
}
func wait(ctx context.Context, input Input, taskID string) error {
	session := input.Session
	generation := input.Generation
	pluginKey := input.PluginKey
	session.Debug(
		"task_plugin subsystem=protocol event=observation_start generation=%d plugin=%q mode=nonstream public_task_id=%q timeout_ms=%d tick_ms=%d",
		generation,
		pluginKey,
		taskID,
		input.Timing.Observation.Milliseconds(),
		input.Timing.Tick.Milliseconds(),
	)
	observationContext, cancelObservation := context.WithTimeout(ctx, input.Timing.Observation)
	defer cancelObservation()
	tickNumber := uint64(0)
	lastStatus := ""
	for {
		loadStarted := session.Now()
		loadContext, cancelLoad := context.WithTimeout(observationContext, input.Timing.Load)
		task, exists, err := session.LoadTask(
			loadContext,
			input.UserID,
			input.PluginKey,
			taskID,
		)
		loadContextErr := loadContext.Err()
		cancelLoad()
		loadElapsed := session.Now().Sub(loadStarted)
		loadOverloaded := errors.Is(loadContextErr, context.DeadlineExceeded) &&
			observationContext.Err() == nil &&
			ctx.Err() == nil
		if loadOverloaded {
			session.Warn(fmt.Sprintf(
				"task protocol database observation overloaded; plugin=%s task=%s",
				input.PluginKey,
				taskID,
			))
			session.Debug(
				"task_plugin subsystem=protocol event=observation_tick generation=%d plugin=%q mode=nonstream tick=%d load_ms=%d overloaded=true",
				generation,
				pluginKey,
				tickNumber,
				loadElapsed.Milliseconds(),
			)
		} else if err != nil || !exists || task == nil {
			if errors.Is(observationContext.Err(), context.DeadlineExceeded) {
				session.Debug("task_plugin subsystem=protocol event=observation_timeout generation=%d plugin=%q "+
					"mode=nonstream last_status=%q",
					generation,
					pluginKey,
					domain.DebugTaskStatus(lastStatus))
				session.Timeout(false, lastStatus)
				return nil
			}
			if err != nil && !errors.Is(err, context.Canceled) {
				session.LogError("task protocol database observation failed")
			}
			if ctx.Err() == nil {
				session.Debug("task_plugin subsystem=protocol event=observation_failed generation=%d plugin=%q "+
					"mode=nonstream stage=load reason=task_unavailable",
					generation,
					pluginKey)
				session.Failure(false, lastStatus)
			} else {
				session.Debug("task_plugin subsystem=protocol event=client_disconnected generation=%d plugin=%q mode=nonstream stage=load", generation, pluginKey)
			}
			return nil
		}
		overloaded := loadOverloaded
		if !loadOverloaded {
			previousStatus := lastStatus
			lastStatus = string(task.Status)
			if lastStatus != previousStatus {
				session.Debug(
					"task_plugin subsystem=protocol event=status_transition generation=%d plugin=%q mode=nonstream tick=%d previous=%q status=%q load_ms=%d",
					generation,
					pluginKey,
					tickNumber,
					domain.DebugTaskStatus(previousStatus),
					domain.DebugTaskStatus(lastStatus),
					loadElapsed.Milliseconds(),
				)
			}
			session.Debug(
				"task_plugin subsystem=protocol event=observation_tick generation=%d plugin=%q mode=nonstream tick=%d status=%q load_ms=%d overloaded=false",
				generation,
				pluginKey,
				tickNumber,
				domain.DebugTaskStatus(lastStatus),
				loadElapsed.Milliseconds(),
			)
		}
		if !loadOverloaded && (task.Status == tasksdomain.TaskStatusSuccess || task.Status == tasksdomain.TaskStatusFailure) {
			if task.Status == tasksdomain.TaskStatusFailure {
				session.Failure(false, string(task.Status))
				return nil
			}
			response, hookElapsed, callErr := session.RenderFinal(observationContext, task)
			if callErr != nil {
				if errors.Is(observationContext.Err(), context.DeadlineExceeded) {
					session.Debug("task_plugin subsystem=protocol event=observation_timeout generation=%d plugin=%q "+
						"mode=nonstream stage=render_final last_status=%q",
						generation,
						pluginKey,
						domain.DebugTaskStatus(lastStatus))
					session.Timeout(false, lastStatus)
					return nil
				}
				if ctx.Err() != nil {
					session.Debug("task_plugin subsystem=protocol event=client_disconnected generation=%d plugin=%q "+
						"mode=nonstream stage=render_final",
						generation,
						pluginKey)
					return nil
				}
				if errors.Is(callErr, domain.ErrRenderAdmission) {
					overloaded = true
					session.Warn(fmt.Sprintf(
						"task protocol final hook overloaded; plugin=%s task=%s",
						input.PluginKey,
						taskID,
					))
				} else {
					session.LogError("task protocol final hook failed")
					session.Debug("task_plugin subsystem=protocol event=observation_failed generation=%d plugin=%q "+
						"mode=nonstream stage=render_final reason=hook_failed elapsed_ms=%d",
						generation,
						pluginKey,
						hookElapsed.Milliseconds(),
					)
					session.Failure(false, lastStatus)
					return nil
				}
			} else {
				session.Debug(
					"task_plugin subsystem=protocol event=render_final generation=%d plugin=%q mode=nonstream status=%q elapsed_ms=%d",
					generation,
					pluginKey,
					domain.DebugTaskStatus(lastStatus),
					hookElapsed.Milliseconds(),
				)
				if err := session.WriteFinal(response); err != nil {
					return err
				}
				session.Debug(
					"task_plugin subsystem=protocol event=observation_complete generation=%d plugin=%q mode=nonstream reason=terminal status=%q ticks=%d",
					generation,
					pluginKey,
					domain.DebugTaskStatus(lastStatus),
					tickNumber+1,
				)
				return nil
			}
		}

		delay := tickDelay(taskID, tickNumber, input.Timing.Tick, input.Timing.Jitter)
		tickNumber++
		if overloaded {
			delay += input.Timing.Tick
		}
		tickTimer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !tickTimer.Stop() {
				select {
				case <-tickTimer.C:
				default:
				}
			}
			session.Debug("task_plugin subsystem=protocol event=client_disconnected generation=%d plugin=%q "+
				"mode=nonstream stage=tick_wait",
				generation,
				pluginKey)
			return nil
		case <-observationContext.Done():
			if !tickTimer.Stop() {
				select {
				case <-tickTimer.C:
				default:
				}
			}
			if errors.Is(observationContext.Err(), context.DeadlineExceeded) {
				session.Debug("task_plugin subsystem=protocol event=observation_timeout generation=%d plugin=%q "+
					"mode=nonstream last_status=%q",
					generation,
					pluginKey,
					domain.DebugTaskStatus(lastStatus))
				session.Timeout(false, lastStatus)
			}
			return nil
		case <-tickTimer.C:
		}
	}
}
func tickDelay(taskID string, tick uint64, base, jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return base
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(taskID))
	_, _ = hash.Write([]byte(":"))
	_, _ = hash.Write([]byte(strconv.FormatUint(tick, 10)))
	return base + time.Duration(hash.Sum64()%uint64(jitter+1))
}
