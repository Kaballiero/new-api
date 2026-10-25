package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	taskrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/QuantumNous/new-api/internal/taskplugins/observe_plugin_task"
	"github.com/QuantumNous/new-api/internal/taskplugins/retrieve_plugin_response"
	tasksdomain "github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/logger"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/taskview"
	"github.com/gin-gonic/gin"
)

type taskPluginObservationSession struct {
	c                          *gin.Context
	pinned                     pluginruntime.PinnedEndpoint
	request                    pluginruntime.ProtocolRequestContext
	deps                       pluginProtocolBridgeDeps
	machine                    *relay.PluginResponsesMachine
	previous                   relay.ProtocolState
	generation                 uint64
	pluginKey, submissionStage string
	outcome                    *taskSubmissionOutcome
	relayInfo                  *relaycommon.RelayInfo
	relayInfoErr               error
	taskErr                    *dto.TaskError
}

func (s *taskPluginObservationSession) Acquire(key string, user, token int) (func(), error) {
	return s.deps.admissions.Acquire(key, user, token)
}
func (s *taskPluginObservationSession) Now() time.Time { return s.deps.now() }
func (s *taskPluginObservationSession) Debug(format string, args ...any) {
	logger.LogDebug(s.c, format, args...)
}
func (s *taskPluginObservationSession) Warn(message string)     { logger.LogWarn(s.c, message) }
func (s *taskPluginObservationSession) LogError(message string) { logger.LogError(s.c, message) }
func (s *taskPluginObservationSession) Disconnected() {
	s.Debug("task_plugin subsystem=protocol event=client_disconnected generation=%d plugin=%q stage=%s", s.generation, s.pluginKey, s.submissionStage)
}
func (s *taskPluginObservationSession) Durable(outcome *tasksdomain.ProtocolSubmission, background bool) error {
	task := s.outcome.Task
	s.Debug("task_plugin subsystem=protocol event=submission_durable generation=%d plugin=%q public_task_id=%q status=%q stream=%t",
		s.generation,
		s.pluginKey,
		task.TaskID,
		domain.DebugTaskStatus(string(task.Status)),
		s.request.Stream)
	createdAt := task.CreatedAt
	if createdAt == 0 {
		createdAt = task.SubmitTime
	}
	if createdAt == 0 {
		createdAt = s.deps.now().Unix()
	}
	s.machine = relay.NewPluginResponsesMachine(task.TaskID, outcome.Model, createdAt, s.deps.protocolLimits)
	if background {
		task.PrivateData.ResponsesBackground = true
		if task.ID != 0 {
			if err := tasksapp.NewStore().MarkResponsesBackground(task); err != nil {
				logger.LogError(s.c, "persist task background flag failed: "+err.Error())
			}
		}
		s.machine.SetBackground(true)
		if s.request.Stream {
			s.Debug("task_plugin subsystem=protocol event=background_stream generation=%d plugin=%q public_task_id=%q", s.generation, s.pluginKey, task.TaskID)
		}
	}
	return nil
}
func (s *taskPluginObservationSession) Pending(task *tasksdomain.Task) error {
	s.Debug("task_plugin subsystem=protocol event=background_return generation=%d plugin=%q public_task_id=%q status=%q",
		s.generation,
		s.pluginKey,
		task.TaskID,
		domain.DebugTaskStatus(string(task.Status)))
	if err := common.WriteClientJSON(s.c, http.StatusOK, s.machine.PendingResponse(string(task.Status))); err != nil {
		s.c.AbortWithStatus(http.StatusBadGateway)
		return domain.ErrObservationHandled
	}
	return nil
}
func (s *taskPluginObservationSession) BeginStream() error {
	created, err := s.machine.CreatedEvent()
	if err != nil {
		s.Debug("task_plugin subsystem=protocol event=observation_failed generation=%d plugin=%q "+
			"mode=stream stage=created_event reason=state_machine_error",
			s.generation,
			s.pluginKey)
		respondPluginProtocolError(s.c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return domain.ErrObservationHandled
	}
	helper.SetEventStreamHeaders(s.c)
	if err = writeTaskPluginProtocolEvent(s.c, created); err != nil {
		s.Debug("task_plugin subsystem=protocol event=client_write_failed generation=%d plugin=%q "+
			"mode=stream stage=created_event",
			s.generation,
			s.pluginKey)
		return domain.ErrObservationHandled
	}
	return nil
}
func (s *taskPluginObservationSession) LoadTask(ctx context.Context, user int, platform, id string) (*tasksdomain.Task, bool, error) {
	p, ok, e := s.deps.loadTask(ctx, user, constant.TaskPlatform(platform), id)
	return taskrecord.ToDomain(p), ok, e
}
func (s *taskPluginObservationSession) RenderFinal(ctx context.Context, task *tasksdomain.Task) (map[string]any, time.Duration, error) {
	v, d, e := renderTaskPluginProtocolFinalResponse(ctx, s.pinned, s.request, taskrecord.FromDomain(task), s.machine, s.deps)
	if errors.Is(e, pluginruntime.ErrCallAdmissionTimeout) {
		e = domain.ErrRenderAdmission
	}
	return v, d, e
}
func (s *taskPluginObservationSession) WriteFinal(value map[string]any) error {
	if e := common.WriteClientJSON(s.c, http.StatusOK, value); e != nil {
		s.c.AbortWithStatus(http.StatusBadGateway)
		return domain.ErrObservationHandled
	}
	return nil
}
func (s *taskPluginObservationSession) Timeout(stream bool, status string) {
	if stream {
		writeTaskPluginProtocolTimeout(s.c, s.machine, status)
	} else {
		writeTaskPluginProtocolTimeoutResponse(s.c, s.machine, status)
	}
}
func (s *taskPluginObservationSession) Failure(stream bool, status string) {
	if stream {
		writeTaskPluginProtocolFailure(s.c, s.machine, status)
	} else {
		writeTaskPluginProtocolFailureResponse(s.c, s.machine, status)
	}
}
func (s *taskPluginObservationSession) WaitTick(ctx context.Context, heartbeat *time.Ticker, delay time.Duration) bool {
	return waitForTaskPluginProtocolTick(s.c, ctx, heartbeat, delay)
}

func (s *taskPluginObservationSession) RenderStream(
	observationContext context.Context,
	task *tasksdomain.Task,
	tickNumber uint64,
) (observe_plugin_task.RenderResult, error) {
	c, pinned, protocolRequest, deps, machine := s.c, s.pinned, s.request, s.deps, s.machine
	generation, pluginKey, taskID := s.generation, s.pluginKey, task.TaskID
	lastStatus := string(task.Status)
	row := taskrecord.FromDomain(task)
	view, viewErr := taskview.BuildTaskPluginView(row)
	if viewErr != nil {
		logger.LogError(c, "build task protocol view failed: "+viewErr.Error())
		writeTaskPluginProtocolFailure(c, machine, lastStatus)
		return observe_plugin_task.RenderResult{}, domain.ErrObservationHandled
	}
	viewValue, viewErr := taskPluginProtocolJSONValue(view)
	if viewErr != nil {
		logger.LogError(c, "encode task protocol view failed: "+viewErr.Error())
		writeTaskPluginProtocolFailure(c, machine, lastStatus)
		return observe_plugin_task.RenderResult{}, domain.ErrObservationHandled
	}
	hookStarted := deps.now()
	rendererContext, contextErr := taskPluginProtocolRendererContext(protocolRequest, pinned, row, deps.artifactContentURL)
	if contextErr != nil {
		logger.LogError(c, "build task protocol renderer context failed")
		writeTaskPluginProtocolFailure(c, machine, lastStatus)
		return observe_plugin_task.RenderResult{}, domain.ErrObservationHandled
	}
	args := []any{rendererContext, viewValue}
	if s.previous.Present {
		previousValue, stateErr := s.previous.PluginValue()
		if stateErr != nil {
			logger.LogError(c, "decode task protocol state failed: "+stateErr.Error())
			writeTaskPluginProtocolFailure(c, machine, lastStatus)
			return observe_plugin_task.RenderResult{}, domain.ErrObservationHandled
		}
		args = append(args, previousValue)
	}
	value, callErr := pinned.Plugin.Engine.CallPathWithAdmissionTimeout(
		observationContext, deps.admissionTimeout, "protocols", []string{pinned.Protocol, "renderEvents"}, args...,
	)
	hookElapsed := deps.now().Sub(hookStarted)
	overloaded := false
	if callErr != nil {
		if errors.Is(observationContext.Err(), context.DeadlineExceeded) {
			logger.LogDebug(c,
				"task_plugin subsystem=protocol event=observation_timeout generation=%d plugin=%q "+
					"mode=stream stage=render_events last_status=%q",
				generation,
				pluginKey,
				domain.DebugTaskStatus(lastStatus))
			writeTaskPluginProtocolTimeout(c, machine, lastStatus)
			return observe_plugin_task.RenderResult{}, domain.ErrObservationHandled
		}
		if c.Request.Context().Err() != nil {
			logger.LogDebug(c,
				"task_plugin subsystem=protocol event=client_disconnected generation=%d plugin=%q "+
					"mode=stream stage=render_events",
				generation,
				pluginKey)
			return observe_plugin_task.RenderResult{}, domain.ErrObservationHandled
		}
		if errors.Is(callErr, pluginruntime.ErrCallAdmissionTimeout) {
			overloaded = true
			logger.LogWarn(c, fmt.Sprintf(
				"task protocol render hook overloaded; plugin=%s task=%s",
				pinned.Plugin.Meta.Key,
				taskID,
			))
		} else {
			logger.LogError(c, "task protocol render hook failed")
			logger.LogDebug(
				c,
				"task_plugin subsystem=protocol event=observation_failed generation=%d plugin=%q "+
					"mode=stream stage=render_events reason=hook_failed elapsed_ms=%d",
				generation,
				pluginKey,
				hookElapsed.Milliseconds(),
			)
			writeTaskPluginProtocolFailure(c, machine, lastStatus)
			return observe_plugin_task.RenderResult{}, domain.ErrObservationHandled
		}
	}
	if !overloaded {
		result, decodeErr := relay.DecodePluginProtocolEventResult(value, deps.protocolLimits)
		if decodeErr != nil {
			logger.LogError(c, "task protocol render result invalid: "+decodeErr.Error())
			writeTaskPluginProtocolFailure(c, machine, lastStatus)
			return observe_plugin_task.RenderResult{}, domain.ErrObservationHandled
		}
		events, applyErr := machine.ApplyTick(result, lastStatus)
		if applyErr != nil {
			logger.LogError(c, "task protocol state transition failed: "+applyErr.Error())
			writeTaskPluginProtocolFailure(c, machine, lastStatus)
			return observe_plugin_task.RenderResult{}, domain.ErrObservationHandled
		}
		logger.LogDebug(
			c,
			"task_plugin subsystem=protocol event=render_events generation=%d plugin=%q mode=stream "+
				"tick=%d status=%q semantic_events=%d wire_events=%d done=%t state_present=%t elapsed_ms=%d",
			generation,
			pluginKey,
			tickNumber,
			domain.DebugTaskStatus(lastStatus),
			len(result.Events),
			len(events),
			result.Done,
			result.State.Present,
			hookElapsed.Milliseconds(),
		)
		for _, event := range events {
			if err := writeTaskPluginProtocolEvent(c, event); err != nil {
				logger.LogDebug(c,
					"task_plugin subsystem=protocol event=client_write_failed generation=%d plugin=%q "+
						"mode=stream stage=event event_type=%q sequence=%d",
					generation,
					pluginKey,
					event.Type,
					event.SequenceNumber)
				return observe_plugin_task.RenderResult{}, domain.ErrObservationHandled
			}
		}
		if taskPluginProtocolEventsTerminal(events) {
			logger.LogDebug(c,
				"task_plugin subsystem=protocol event=observation_complete generation=%d plugin=%q "+
					"mode=stream reason=terminal status=%q ticks=%d",
				generation,
				pluginKey,
				domain.DebugTaskStatus(lastStatus),
				tickNumber+1)
			return observe_plugin_task.RenderResult{Done: true, HookElapsed: hookElapsed}, nil
		}
		s.previous = result.State
	}

	return observe_plugin_task.RenderResult{Overloaded: overloaded, HookElapsed: hookElapsed}, nil

}

func (s *taskPluginObservationSession) RespondError(err error) {
	if errors.Is(err, domain.ErrObservationHandled) {
		return
	}
	c, pinned := s.c, s.pinned
	generation, pluginKey := s.generation, s.pluginKey
	if errors.Is(err, domain.ErrObservationLimitExceeded) {
		s.Debug("task_plugin subsystem=protocol event=admission_rejected generation=%d plugin=%q reason=observation_limit", generation, pluginKey)
		respondPluginProtocolError(c, http.StatusTooManyRequests, "rate_limit_exceeded", "Too many active task observations")
		return
	}
	if errors.Is(err, domain.ErrInvalidObservationIdentity) {
		s.Debug("task_plugin subsystem=protocol event=admission_rejected generation=%d plugin=%q reason=invalid_identity", generation, pluginKey)
		respondPluginProtocolError(c, http.StatusUnauthorized, "authentication_error", "Authentication failed")
		return
	}
	relayInfoErr, taskErr, submissionStage, outcome, relayInfo := s.relayInfoErr, s.taskErr, s.submissionStage, s.outcome, s.relayInfo
	if relayInfoErr != nil {
		err := relayInfoErr
		logger.LogError(c, "build task protocol relay info failed: "+err.Error())
		logger.LogDebug(c,
			"task_plugin subsystem=protocol event=bridge_failed generation=%d plugin=%q "+
				"stage=relay_info reason=invalid_context",
			generation,
			pluginKey)
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return
	}
	if submissionStage == "origin_task" && taskErr != nil {
		logger.LogDebug(
			c,
			"task_plugin subsystem=protocol event=bridge_failed generation=%d plugin=%q stage=origin_task code=%q status=%d",
			generation,
			pluginKey,
			taskErr.Code,
			taskErr.StatusCode,
		)
		respondPluginProtocolSubmissionError(c, taskErr)
		return
	}

	if taskErr != nil {
		logger.LogDebug(
			c,
			"task_plugin subsystem=protocol event=submission_failed generation=%d plugin=%q code=%q status=%d local=%t",
			generation,
			pluginKey,
			taskErr.Code,
			taskErr.StatusCode,
			taskErr.LocalError,
		)
		respondPluginProtocolSubmissionError(c, taskErr)
		return
	}
	if outcome == nil || outcome.Task == nil || outcome.RelayInfo == nil ||
		outcome.Task.UserId != relayInfo.UserId ||
		outcome.Task.Platform != constant.TaskPlatform(pinned.Plugin.Meta.Key) {
		logger.LogError(c, "task protocol submission returned an invalid durable outcome")
		logger.LogDebug(c,
			"task_plugin subsystem=protocol event=submission_failed generation=%d plugin=%q "+
				"reason=invalid_durable_outcome",
			generation,
			pluginKey)
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", "Task protocol request failed")
		return
	}
}

type taskPluginRetrieveSession struct {
	deps       pluginProtocolBridgeDeps
	plugin     *pluginruntime.LoadedPlugin
	generation *pluginruntime.RoutingGeneration
}

func (s *taskPluginRetrieveSession) Owned(user int, id string) (*tasksdomain.Task, bool, error) {
	p, ok, e := s.deps.getByTaskId(user, id)
	return taskrecord.ToDomain(p), ok, e
}
func (s *taskPluginRetrieveSession) Protocol(platform string) retrieve_plugin_response.Availability {
	p, g, ok := s.deps.resolvePlugin(constant.TaskPlatform(platform))
	s.plugin, s.generation = p, g
	a := retrieve_plugin_response.Availability{Found: ok && p != nil}
	if !a.Found {
		return a
	}
	for _, claim := range p.Meta.Protocols {
		if claim.Name == "openai_responses" {
			a.Claims = true
			break
		}
	}
	a.Sync = p.Meta.ProtocolSupports("openai_responses", "sync")
	a.Background = p.Meta.ProtocolSupports("openai_responses", "background")
	return a
}
