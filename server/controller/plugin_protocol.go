package controller

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/i18n"
	plugintransport "github.com/QuantumNous/new-api/internal/taskplugins/transport"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	taskrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	pluginsruntime "github.com/QuantumNous/new-api/internal/app/taskpluginsruntime"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/QuantumNous/new-api/internal/taskplugins/observe_plugin_task"
	"github.com/QuantumNous/new-api/internal/taskplugins/retrieve_plugin_response"
	tasksdomain "github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	taskjsplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/taskregistry"
	"github.com/QuantumNous/new-api/relay/taskview"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type pluginProtocolBridgeDeps struct {
	submit             func(*gin.Context, *relaycommon.RelayInfo) (*taskSubmissionOutcome, *dto.TaskError)
	loadTask           func(context.Context, int, constant.TaskPlatform, string) (*model.Task, bool, error)
	now                func() time.Time
	admissions         *domain.ObservationLimiter
	protocolLimits     relay.PluginProtocolLimits
	artifactContentURL func(taskID, artifactKey string) (string, error)
	submissionTimeout  time.Duration
	observationTimeout time.Duration
	loadTimeout        time.Duration
	tickInterval       time.Duration
	tickJitter         time.Duration
	heartbeatInterval  time.Duration
	admissionTimeout   time.Duration
	getByTaskId        func(int, string) (*model.Task, bool, error)
	resolvePlugin      func(constant.TaskPlatform) (*pluginruntime.LoadedPlugin, *pluginruntime.RoutingGeneration, bool)
}

func defaultPluginProtocolBridgeDeps() pluginProtocolBridgeDeps {
	timeout := time.Duration(constant.TaskPluginProtocolTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	tick := time.Duration(constant.TaskPluginProtocolTickMilliseconds) * time.Millisecond
	if tick <= 0 {
		tick = 2 * time.Second
	}
	jitter := max(time.Duration(constant.TaskPluginProtocolTickJitterMilliseconds)*time.Millisecond, 0)
	heartbeat := time.Duration(constant.TaskPluginProtocolHeartbeatSeconds) * time.Second
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	loadTimeout := 5 * time.Second
	if halfHeartbeat := heartbeat / 2; halfHeartbeat > 0 && halfHeartbeat < loadTimeout {
		loadTimeout = halfHeartbeat
	}
	return pluginProtocolBridgeDeps{
		submit:             executeTaskSubmission,
		loadTask:           tasksapp.NewStore().GetTaskForProtocolObservation,
		now:                time.Now,
		admissions:         pluginsruntime.ObservationAdmissions(),
		protocolLimits:     relay.DefaultPluginProtocolLimits(),
		artifactContentURL: service.BuildTaskArtifactContentURL,
		submissionTimeout:  timeout,
		observationTimeout: timeout,
		loadTimeout:        loadTimeout,
		tickInterval:       tick,
		tickJitter:         jitter,
		heartbeatInterval:  heartbeat,
		admissionTimeout:   pluginruntime.DefaultCallTimeout,
		getByTaskId:        tasksapp.NewStore().GetByTaskId,
		resolvePlugin:      resolveTaskPluginForProtocolRetrieve,
	}
}

func (d pluginProtocolBridgeDeps) withDefaults() pluginProtocolBridgeDeps {
	defaults := defaultPluginProtocolBridgeDeps()
	if d.submit == nil {
		d.submit = defaults.submit
	}
	if d.loadTask == nil {
		d.loadTask = defaults.loadTask
	}
	if d.now == nil {
		d.now = defaults.now
	}
	if d.admissions == nil {
		d.admissions = defaults.admissions
	}
	if d.artifactContentURL == nil {
		d.artifactContentURL = defaults.artifactContentURL
	}
	if d.submissionTimeout <= 0 {
		d.submissionTimeout = defaults.submissionTimeout
	}
	if d.observationTimeout <= 0 {
		d.observationTimeout = defaults.observationTimeout
	}
	if d.loadTimeout <= 0 {
		d.loadTimeout = defaults.loadTimeout
	}
	if d.tickInterval <= 0 {
		d.tickInterval = defaults.tickInterval
	}
	if d.tickJitter < 0 {
		d.tickJitter = 0
	}
	if d.heartbeatInterval <= 0 {
		d.heartbeatInterval = defaults.heartbeatInterval
	}
	if halfHeartbeat := d.heartbeatInterval / 2; halfHeartbeat > 0 && d.loadTimeout > halfHeartbeat {
		d.loadTimeout = halfHeartbeat
	}
	if d.admissionTimeout <= 0 {
		d.admissionTimeout = defaults.admissionTimeout
	}
	if d.getByTaskId == nil {
		d.getByTaskId = defaults.getByTaskId
	}
	if d.resolvePlugin == nil {
		d.resolvePlugin = defaults.resolvePlugin
	}
	return d
}

func resolveTaskPluginForProtocolRetrieve(platform constant.TaskPlatform) (*pluginruntime.LoadedPlugin, *pluginruntime.RoutingGeneration, bool) {
	generation := pluginruntime.DefaultRegistry.Generation()
	plugin, ok := taskregistry.ResolveTaskPluginForPlatform(generation, platform)
	return plugin, generation, ok
}

func serveTaskPluginProtocol(
	c *gin.Context,
	pinned pluginruntime.PinnedEndpoint,
	deps pluginProtocolBridgeDeps,
) {
	deps = deps.withDefaults()
	generation := uint64(0)
	if pinned.Generation != nil {
		generation = pinned.Generation.Number
	}
	pluginKey := ""
	if pinned.Plugin != nil {
		pluginKey = pinned.Plugin.Meta.Key
	}
	logger.LogDebug(
		c,
		"task_plugin subsystem=protocol event=bridge_start generation=%d plugin=%q protocol=%q model=%q",
		generation,
		pluginKey,
		pinned.Protocol,
		c.GetString("resolved_task_model"),
	)
	if !pluginruntime.SupportsHostProtocol(pinned.Protocol) {
		logger.LogDebug(c, "task_plugin subsystem=protocol event=bridge_rejected generation=%d plugin=%q reason=unsupported_protocol", generation, pluginKey)
		respondPluginProtocolError(c, http.StatusNotImplemented, "task_protocol_not_available", i18n.Translate(plugintransport.Language(c), i18n.MsgTaskPluginProtocolUnavailable))
		return
	}
	requestValue, exists := c.Get(pluginruntime.ContextKeyProtocolRequest)
	protocolRequest, ok := requestValue.(pluginruntime.ProtocolRequestContext)
	if !exists || !ok || protocolRequest.Protocol != pinned.Protocol {
		logger.LogDebug(c, "task_plugin subsystem=protocol event=bridge_rejected generation=%d plugin=%q reason=invalid_protocol_context", generation, pluginKey)
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", i18n.Translate(plugintransport.Language(c), i18n.MsgTaskPluginProtocolFailed))
		return
	}
	if definition, known := pluginruntime.HostProtocol(pinned.Protocol); known && len(definition.DefinedModes()) > 0 && pinned.Plugin != nil {
		background := false
		if body, ok := protocolRequest.Body.(map[string]any); ok && body["kind"] == string(pluginruntime.BodyJSON) {
			if requestBody, ok := body["value"].(map[string]any); ok {
				background, _ = requestBody["background"].(bool)
			}
		}
		missing := false
		if protocolRequest.Stream && !pinned.Plugin.Meta.ProtocolSupports(pinned.Protocol, "stream") {
			missing = true
		}
		if background && !pinned.Plugin.Meta.ProtocolSupports(pinned.Protocol, "background") {
			missing = true
		}
		if !protocolRequest.Stream && !background && !pinned.Plugin.Meta.ProtocolSupports(pinned.Protocol, "sync") {
			missing = true
		}
		if missing {
			logger.LogError(c, "pinned task plugin does not support the requested protocol form")
			respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", i18n.Translate(plugintransport.Language(c), i18n.MsgTaskPluginProtocolFailed))
			return
		}
	}
	logger.LogDebug(
		c,
		"task_plugin subsystem=protocol event=request_ready generation=%d plugin=%q protocol=%q stream=%t",
		generation,
		pluginKey,
		protocolRequest.Protocol,
		protocolRequest.Stream,
	)

	background := false
	if body, ok := protocolRequest.Body.(map[string]any); ok && body["kind"] == string(pluginruntime.BodyJSON) {
		if requestBody, ok := body["value"].(map[string]any); ok {
			background, _ = requestBody["background"].(bool)
		}
	}
	session := &taskPluginObservationSession{c: c, pinned: pinned, request: protocolRequest, deps: deps, generation: generation, pluginKey: pluginKey}
	_ = pluginsruntime.Observer.Execute(c.Request.Context(), observe_plugin_task.Input{
		Session:    session,
		UserID:     common.GetContextKeyInt(c, constant.ContextKeyUserId),
		TokenID:    common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		PluginKey:  pluginKey,
		Generation: generation,
		Stream:     protocolRequest.Stream,
		Background: background,
		Timing: domain.ObservationTiming{
			Submission:  deps.submissionTimeout,
			Observation: deps.observationTimeout,
			Load:        deps.loadTimeout,
			Tick:        deps.tickInterval,
			Jitter:      deps.tickJitter,
			Heartbeat:   deps.heartbeatInterval,
		},
	})
}

func waitForTaskPluginProtocolTick(
	c *gin.Context,
	observationContext context.Context,
	heartbeatTicker *time.Ticker,
	delay time.Duration,
) bool {
	tickTimer := time.NewTimer(delay)
	defer tickTimer.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return false
		case <-observationContext.Done():
			return false
		case <-heartbeatTicker.C:
			helper.ExtendWriteDeadline(c)
			if err := writeTaskPluginProtocolHeartbeat(c); err != nil {
				return false
			}
		case <-tickTimer.C:
			return true
		}
	}
}

func renderTaskPluginProtocolFinalResponse(
	ctx context.Context,
	pinned pluginruntime.PinnedEndpoint,
	protocolRequest pluginruntime.ProtocolRequestContext,
	task *model.Task,
	machine *relay.PluginResponsesMachine,
	deps pluginProtocolBridgeDeps,
) (map[string]any, time.Duration, error) {
	view, err := taskview.BuildTaskPluginView(task)
	if err != nil {
		return nil, 0, err
	}
	viewValue, err := taskPluginProtocolJSONValue(view)
	if err != nil {
		return nil, 0, err
	}
	rendererContext, err := taskPluginProtocolRendererContext(
		protocolRequest,
		pinned,
		task,
		deps.artifactContentURL,
	)
	if err != nil {
		return nil, 0, err
	}
	hookStarted := deps.now()
	payload, err := pinned.Plugin.Engine.CallPathWithAdmissionTimeout(
		ctx,
		deps.admissionTimeout,
		"protocols",
		[]string{pinned.Protocol, "renderFinal"},
		rendererContext,
		viewValue,
	)
	hookElapsed := deps.now().Sub(hookStarted)
	if err != nil {
		return nil, hookElapsed, err
	}
	response, err := machine.FinalResponse(payload, string(task.Status))
	if err != nil {
		return nil, hookElapsed, err
	}
	return response, hookElapsed, nil
}

func renderTaskPluginProtocolEventsResponse(
	ctx context.Context,
	pinned pluginruntime.PinnedEndpoint,
	protocolRequest pluginruntime.ProtocolRequestContext,
	task *model.Task,
	machine *relay.PluginResponsesMachine,
	deps pluginProtocolBridgeDeps,
) (map[string]any, time.Duration, error) {
	view, err := taskview.BuildTaskPluginView(task)
	if err != nil {
		return nil, 0, err
	}
	viewValue, err := taskPluginProtocolJSONValue(view)
	if err != nil {
		return nil, 0, err
	}
	rendererContext, err := taskPluginProtocolRendererContext(
		protocolRequest,
		pinned,
		task,
		deps.artifactContentURL,
	)
	if err != nil {
		return nil, 0, err
	}
	hookStarted := deps.now()
	value, err := pinned.Plugin.Engine.CallPathWithAdmissionTimeout(
		ctx,
		deps.admissionTimeout,
		"protocols",
		[]string{pinned.Protocol, "renderEvents"},
		rendererContext,
		viewValue,
	)
	hookElapsed := deps.now().Sub(hookStarted)
	if err != nil {
		return nil, hookElapsed, err
	}
	result, err := relay.DecodePluginProtocolEventResult(value, deps.protocolLimits)
	if err != nil {
		return nil, hookElapsed, err
	}
	response, err := machine.FinalFromEvents(result, string(task.Status))
	if err != nil {
		return nil, hookElapsed, err
	}
	return response, hookElapsed, nil
}

func RetrieveTaskPluginResponse(c *gin.Context) {
	retrieveTaskPluginResponse(c, defaultPluginProtocolBridgeDeps())
}

func retrieveTaskPluginResponse(c *gin.Context, deps pluginProtocolBridgeDeps) {
	deps = deps.withDefaults()
	responseID := strings.TrimSpace(c.Param("response_id"))
	if !strings.HasPrefix(responseID, "resp_") {
		writeTaskPluginResponseNotFound(c, responseID, "bad_prefix")
		return
	}
	taskID := "task_" + strings.TrimPrefix(responseID, "resp_")
	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	logger.LogDebug(c, "task_plugin subsystem=protocol event=retrieve_start response_id=%q public_task_id=%q", responseID, taskID)

	session := &taskPluginRetrieveSession{deps: deps}
	selection, err := pluginsruntime.Retriever.Execute(c.Request.Context(), retrieve_plugin_response.Input{
		Session: session,
		UserID:  userID,
		TaskID:  taskID,
	})
	if err != nil {
		var failure *domain.RetrieveError
		if !errors.As(err, &failure) || failure.Reason == "lookup_error" {
			logger.LogError(c, "task protocol retrieve lookup failed")
			logger.LogDebug(c, "task_plugin subsystem=protocol event=retrieve_failed reason=lookup_error public_task_id=%q", taskID)
			respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", i18n.Translate(plugintransport.Language(c), i18n.MsgTaskPluginProtocolFailed))
			return
		}
		writeTaskPluginResponseNotFound(c, responseID, failure.Reason)
		return
	}
	task := taskrecord.FromDomain(selection.Task)
	plugin, generation := session.plugin, session.generation

	common.BindClientResponseModel(c, task.Properties.OriginModelName)

	generationNumber := uint64(0)
	if generation != nil {
		generationNumber = generation.Number
	}
	createdAt := task.CreatedAt
	if createdAt == 0 {
		createdAt = task.SubmitTime
	}
	if createdAt == 0 {
		createdAt = deps.now().Unix()
	}
	machine := relay.NewPluginResponsesMachine(
		task.TaskID,
		task.Properties.OriginModelName,
		createdAt,
		deps.protocolLimits,
	)
	machine.SetBackground(task.PrivateData.ResponsesBackground)
	pinned := pluginruntime.PinnedEndpoint{
		Generation: generation,
		Plugin:     plugin,
		Protocol:   "openai_responses",
		Model:      task.Properties.OriginModelName,
	}
	protocolRequest := pluginruntime.ProtocolRequestContext{
		RouteRequestContext: pluginruntime.RouteRequestContext{
			Path:   c.Request.URL.Path,
			Method: http.MethodGet,
			Params: map[string]string{"response_id": responseID},
			Query:  c.Request.URL.Query(),
			Body:   map[string]any{"kind": string(pluginruntime.BodyNone)},
		},
		Protocol:  "openai_responses",
		Operation: "retrieve",
		Model:     task.Properties.OriginModelName,
	}

	if selection.Mode == "failure" {
		logger.LogDebug(c,
			"task_plugin subsystem=protocol event=retrieve_final generation=%d plugin=%q "+
				"public_task_id=%q status=%q",
			generationNumber,
			plugin.Meta.Key,
			task.TaskID,
			domain.DebugTaskStatus(string(task.Status)))
		writeTaskPluginProtocolFailureResponse(c, machine, string(task.Status))
		return
	}
	if selection.Mode == "pending" {
		logger.LogDebug(c,
			"task_plugin subsystem=protocol event=retrieve_pending generation=%d plugin=%q "+
				"public_task_id=%q status=%q",
			generationNumber,
			plugin.Meta.Key,
			task.TaskID,
			domain.DebugTaskStatus(string(task.Status)))
		if err := common.WriteClientJSON(c, http.StatusOK, machine.PendingResponse(string(task.Status))); err != nil {
			c.AbortWithStatus(http.StatusBadGateway)
			return
		}
		return
	}

	var (
		response    map[string]any
		hookElapsed time.Duration
		renderErr   error
	)
	if selection.Mode == "final" {
		response, hookElapsed, renderErr = renderTaskPluginProtocolFinalResponse(
			c.Request.Context(),
			pinned,
			protocolRequest,
			task,
			machine,
			deps,
		)
	} else {
		response, hookElapsed, renderErr = renderTaskPluginProtocolEventsResponse(
			c.Request.Context(),
			pinned,
			protocolRequest,
			task,
			machine,
			deps,
		)
	}
	if renderErr != nil {
		logger.LogError(c, "task protocol retrieve render failed")
		logger.LogDebug(
			c,
			"task_plugin subsystem=protocol event=retrieve_failed generation=%d plugin=%q public_task_id=%q stage=render_final elapsed_ms=%d",
			generationNumber,
			plugin.Meta.Key,
			task.TaskID,
			hookElapsed.Milliseconds(),
		)
		writeTaskPluginProtocolFailureResponse(c, machine, string(task.Status))
		return
	}
	logger.LogDebug(
		c,
		"task_plugin subsystem=protocol event=retrieve_final generation=%d plugin=%q public_task_id=%q status=%q elapsed_ms=%d",
		generationNumber,
		plugin.Meta.Key,
		task.TaskID,
		domain.DebugTaskStatus(string(task.Status)),
		hookElapsed.Milliseconds(),
	)
	if err := common.WriteClientJSON(c, http.StatusOK, response); err != nil {
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
}

func writeTaskPluginResponseNotFound(c *gin.Context, responseID, reason string) {
	logger.LogDebug(c, "task_plugin subsystem=protocol event=retrieve_not_found reason=%s response_id=%q", reason, responseID)
	respondPluginProtocolError(c, http.StatusNotFound, "not_found", i18n.Translate(plugintransport.Language(c), i18n.MsgTaskPluginResponseNotFound, map[string]any{"ID": responseID}))
}

func writeTaskPluginProtocolHeartbeat(c *gin.Context) error {
	if _, err := c.Writer.Write([]byte(": PING\n")); err != nil {
		return err
	}
	return helper.FlushWriter(c)
}

func writeTaskPluginProtocolFailure(
	c *gin.Context,
	machine *relay.PluginResponsesMachine,
	taskStatus string,
) {
	failed, err := machine.FailureEvent(taskStatus)
	if err != nil {
		logger.LogError(c, "task protocol failure event failed: "+err.Error())
		return
	}
	_ = writeTaskPluginProtocolEvent(c, failed)
}

func writeTaskPluginProtocolTimeout(
	c *gin.Context,
	machine *relay.PluginResponsesMachine,
	taskStatus string,
) {
	incomplete, err := machine.TimeoutEvent(taskStatus)
	if err != nil {
		logger.LogError(c, "task protocol timeout event failed: "+err.Error())
		return
	}
	_ = writeTaskPluginProtocolEvent(c, incomplete)
}

func writeTaskPluginProtocolFailureResponse(
	c *gin.Context,
	machine *relay.PluginResponsesMachine,
	taskStatus string,
) {
	if taskStatus == string(model.TaskStatusFailure) {
		response, err := machine.FinalResponse(nil, taskStatus)
		if err != nil {
			logger.LogError(c, "task protocol terminal failure response failed: "+err.Error())
			respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", i18n.Translate(plugintransport.Language(c), i18n.MsgTaskPluginProtocolFailed))
			return
		}
		if err := common.WriteClientJSON(c, http.StatusOK, response); err != nil {
			c.AbortWithStatus(http.StatusBadGateway)
			return
		}
		return
	}
	response, err := machine.FailureResponse(taskStatus)
	if err != nil {
		logger.LogError(c, "task protocol failure response failed: "+err.Error())
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", i18n.Translate(plugintransport.Language(c), i18n.MsgTaskPluginProtocolFailed))
		return
	}
	if err := common.WriteClientJSON(c, http.StatusOK, response); err != nil {
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
}

func writeTaskPluginProtocolTimeoutResponse(
	c *gin.Context,
	machine *relay.PluginResponsesMachine,
	lastStatus string,
) {
	response, err := machine.TimeoutResponse(lastStatus)
	if err != nil {
		logger.LogError(c, "task protocol timeout response failed: "+err.Error())
		respondPluginProtocolError(c, http.StatusInternalServerError, "task_protocol_error", i18n.Translate(plugintransport.Language(c), i18n.MsgTaskPluginProtocolFailed))
		return
	}
	if err := common.WriteClientJSON(c, http.StatusOK, response); err != nil {
		c.AbortWithStatus(http.StatusBadGateway)
		return
	}
}

func taskPluginProtocolJSONValue(value any) (any, error) {
	encoded, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err = common.Unmarshal(encoded, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func taskPluginProtocolRendererContext(
	request pluginruntime.ProtocolRequestContext,
	pinned pluginruntime.PinnedEndpoint,
	task *model.Task,
	artifactContentURL func(taskID, artifactKey string) (string, error),
) (map[string]any, error) {
	rendererContext := request.JSValue()
	if task == nil || task.Status != model.TaskStatusSuccess {
		return rendererContext, nil
	}
	if pinned.Plugin == nil {
		return nil, errors.New("task artifact projection is unavailable")
	}

	artifacts, err := taskjsplugin.New(pinned.Plugin).ListArtifacts(task)
	if err != nil {
		return nil, fmt.Errorf("project task artifacts: %w", err)
	}
	projected := make([]tasksdomain.Artifact, len(artifacts))
	for i, artifact := range artifacts {
		projected[i] = tasksdomain.Artifact{Key: artifact.Key, Type: artifact.Type, MimeType: artifact.MimeType}
	}
	_, err = tasksdomain.ValidateArtifacts(projected)
	if err != nil {
		return nil, err
	}
	if len(artifacts) > 0 && artifactContentURL == nil {
		return nil, errors.New("task artifact projection is unavailable")
	}

	rendererArtifacts := make(map[string]any, len(artifacts))
	for _, artifact := range artifacts {
		contentURL, buildErr := artifactContentURL(task.TaskID, artifact.Key)
		if buildErr != nil {
			return nil, fmt.Errorf("build task artifact content URL: %w", buildErr)
		}
		item := map[string]any{
			"key":  artifact.Key,
			"type": artifact.Type,
			"url":  contentURL,
		}
		if artifact.MimeType != "" {
			item["mimeType"] = artifact.MimeType
		}
		rendererArtifacts[artifact.Key] = item
	}
	rendererContext["artifacts"] = rendererArtifacts
	return rendererContext, nil
}

func taskPluginProtocolEventsTerminal(events []dto.PluginResponsesStreamEvent) bool {
	for _, event := range events {
		switch event.Type {
		case "response.completed", "response.failed", "response.incomplete":
			return true
		}
	}
	return false
}

func writeTaskPluginProtocolEvent(c *gin.Context, event dto.PluginResponsesStreamEvent) error {
	encoded, err := common.Marshal(event)
	if err != nil {
		return err
	}
	encoded, err = common.ProjectClientResponse(c, encoded)
	if err != nil {
		return err
	}
	helper.ExtendWriteDeadline(c)
	if _, err = c.Writer.Write([]byte("event: " + event.Type + "\n")); err != nil {
		return err
	}
	if _, err = c.Writer.Write([]byte("data: " + string(encoded) + "\n\n")); err != nil {
		return err
	}
	if err = helper.FlushWriter(c); err != nil {
		return err
	}
	logger.LogDebug(
		c,
		"task_plugin subsystem=protocol event=sse_event_sent event_type=%q sequence=%d",
		event.Type,
		event.SequenceNumber,
	)
	return nil
}

func respondPluginProtocolSubmissionError(c *gin.Context, taskErr *dto.TaskError) {
	status := http.StatusInternalServerError
	if taskErr != nil && taskErr.StatusCode >= 400 && taskErr.StatusCode <= 599 {
		status = taskErr.StatusCode
	}
	switch status {
	case http.StatusBadRequest:
		message := "Invalid task protocol request"
		if taskErr != nil && taskErr.Message != "" && (taskErr.Code == "invalid_request" || strings.HasPrefix(taskErr.Code, "invalid_request")) {
			message = taskErr.Message
		}
		respondPluginProtocolError(c, status, "invalid_request_error", message)
	case http.StatusUnauthorized:
		respondPluginProtocolError(c, status, "authentication_error", i18n.Translate(plugintransport.Language(c), i18n.MsgTaskPluginAuthentication))
	case http.StatusForbidden:
		respondPluginProtocolError(c, status, "permission_denied", "Task protocol request was denied")
	case http.StatusTooManyRequests:
		respondPluginProtocolError(c, status, "rate_limit_exceeded", "Too many requests")
	default:
		respondPluginProtocolError(c, status, "task_protocol_error", i18n.Translate(plugintransport.Language(c), i18n.MsgTaskPluginProtocolFailed))
	}
}

func respondPluginProtocolError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "new_api_error",
			"code":    code,
		},
	})
}
