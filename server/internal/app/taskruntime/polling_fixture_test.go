package taskruntime

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/constant"
	taskplugins "github.com/QuantumNous/new-api/internal/adapter/gateway/taskplugins"
	taskstore "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	effects "github.com/QuantumNous/new-api/internal/adapter/taskruntime"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/poll_tasks"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// These test ports preserve existing provider fakes while exercising the real
// polling scenario. No production registration or forwarding API is required.
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL, key string, task *model.Task, proxy string) (*http.Response, error)
	ParseTaskResult(task *model.Task, response *http.Response, body []byte) (*relaycommon.TaskInfo, error)
	AdjustBillingOnComplete(task *model.Task, result *relaycommon.TaskInfo) int
}

type BatchTaskResult = channel.BatchTaskResult
type TaskPollSummary = poll_tasks.TaskPollSummary

type BatchTaskPollingAdaptor interface {
	TaskPollingAdaptor
	FetchMode() string
	FetchBatchTasks(baseURL, key string, tasks []*model.Task, proxy string) (*http.Response, error)
	ParseBatchResult(tasks []*model.Task, response *http.Response, body []byte) (map[string]*BatchTaskResult, error)
}

var GetTaskAdaptorFunc func(constant.TaskPlatform) TaskPollingAdaptor

type fixtureFactory struct {
	open func(constant.TaskPlatform) TaskPollingAdaptor
}

func (f fixtureFactory) Open(platform string) poll_tasks.Adaptor {
	if f.open == nil {
		return nil
	}
	adaptor := f.open(constant.TaskPlatform(platform))
	if adaptor == nil {
		return nil
	}
	p := fixturePoller{adaptor: adaptor}
	if batch, ok := adaptor.(BatchTaskPollingAdaptor); ok {
		return fixtureBatchPoller{fixturePoller: p, batch: batch}
	}
	return p
}

type fixturePoller struct{ adaptor TaskPollingAdaptor }

func (p fixturePoller) Init(baseURL, key string) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: baseURL}}
	info.ApiKey = key
	p.adaptor.Init(info)
}
func (p fixturePoller) FetchTask(baseURL, key string, task *domain.Task, proxy string) (*poll_tasks.Response, error) {
	response, err := p.adaptor.FetchTask(baseURL, key, record.FromDomain(task), proxy)
	if err != nil {
		return nil, err
	}
	return &poll_tasks.Response{StatusCode: response.StatusCode, Headers: response.Header, Body: response.Body}, nil
}
func (p fixturePoller) ParseTaskResult(task *domain.Task, response *poll_tasks.Response, body []byte) (*domain.TaskInfo, error) {
	info, err := p.adaptor.ParseTaskResult(record.FromDomain(task), &http.Response{StatusCode: response.StatusCode, Header: response.Headers, Body: response.Body}, body)
	return taskplugins.TaskInfoToDomain(info), err
}
func (p fixturePoller) AdjustBillingOnComplete(task *domain.Task, result *domain.TaskInfo) int {
	return p.adaptor.AdjustBillingOnComplete(record.FromDomain(task), taskplugins.TaskInfoFromDomain(result))
}

type fixtureBatchPoller struct {
	fixturePoller
	batch BatchTaskPollingAdaptor
}

func (p fixtureBatchPoller) FetchMode() string { return p.batch.FetchMode() }
func (p fixtureBatchPoller) FetchBatchTasks(baseURL, key string, tasks []*domain.Task, proxy string) (*poll_tasks.Response, error) {
	rows := make([]*model.Task, len(tasks))
	for i := range tasks {
		rows[i] = record.FromDomain(tasks[i])
	}
	response, err := p.batch.FetchBatchTasks(baseURL, key, rows, proxy)
	if err != nil {
		return nil, err
	}
	return &poll_tasks.Response{StatusCode: response.StatusCode, Headers: response.Header, Body: response.Body}, nil
}
func (p fixtureBatchPoller) ParseBatchResult(tasks []*domain.Task, response *poll_tasks.Response, body []byte) (map[string]*poll_tasks.BatchResult, error) {
	rows := make([]*model.Task, len(tasks))
	for i := range tasks {
		rows[i] = record.FromDomain(tasks[i])
	}
	results, err := p.batch.ParseBatchResult(rows, &http.Response{StatusCode: response.StatusCode, Header: response.Headers, Body: response.Body}, body)
	if err != nil {
		return nil, err
	}
	if results == nil {
		return nil, nil
	}
	mapped := make(map[string]*poll_tasks.BatchResult, len(results))
	for id, result := range results {
		if result == nil {
			mapped[id] = nil
			continue
		}
		mapped[id] = &poll_tasks.BatchResult{TaskInfo: *taskplugins.TaskInfoToDomain(&result.TaskInfo), SubmitTime: result.SubmitTime, StartTime: result.StartTime, FinishTime: result.FinishTime, Data: result.Data}
	}
	return mapped, nil
}

// fixtureInventory supplies the exact stale records already selected by each
// regression; claim writes still use the production store and database.
type fixtureInventory struct {
	taskstore.PollingStore
	tasks    []*domain.Task
	pageRead bool
}

func (s *fixtureInventory) UnfinishedPage(limit int, afterID int64) []*domain.Task {
	if s.pageRead {
		return nil
	}
	s.pageRead = true
	return s.tasks
}

type fixtureChannels struct{ override *model.Channel }

func (c fixtureChannels) Get(id int) (poll_tasks.Channel, error) {
	if c.override == nil {
		return (effects.PollChannels{}).Get(id)
	}
	ch := c.override
	return poll_tasks.Channel{Id: ch.Id, BaseURL: ch.GetBaseURL(), FallbackBaseURL: constant.GetChannelBaseURL(ch.Type), Key: ch.Key, Proxy: ch.GetSetting().Proxy, DisablePollingSleep: ch.GetOtherSettings().DisableTaskPollingSleep}, nil
}

func fixtureUsecase(store poll_tasks.Store, channels poll_tasks.ChannelReader, factory poll_tasks.Factory, timeout func() int, completion poll_tasks.Completion) *poll_tasks.Usecase {
	if completion == nil {
		completion = tasksapp.NewBilling()
	}
	return poll_tasks.NewUsecase(store, channels, factory, completion, tasksapp.NewReconciler(), effects.Codec{}, effects.Diagnostics{},
		poll_tasks.Limits{TimeoutMinutes: timeout, QueryLimit: func() int { return constant.TaskQueryLimit }, MaxFailures: func() int { return constant.TaskPollMaxFailures }}, time.Now, gopool.Go, taskcommon.BuildProxyURL, effects.TaskText{})
}

func RunTaskPollingOnce(ctx context.Context, report func(processed, total int)) TaskPollSummary {
	store := taskstore.PollingStore{Store: taskstore.NewStore(func() *gorm.DB { return model.DB })}
	var factory poll_tasks.Factory
	if GetTaskAdaptorFunc != nil {
		factory = fixtureFactory{open: GetTaskAdaptorFunc}
	}
	u := fixtureUsecase(store, fixtureChannels{}, factory, func() int { return constant.TaskTimeoutMinutes }, nil)
	summary, _ := u.Execute(ctx, report)
	return summary
}

func runSelectedPolling(ctx context.Context, platform constant.TaskPlatform, factory fixtureFactory, channels map[int][]string, tasks map[string]*model.Task, override *model.Channel) error {
	selected := make([]*domain.Task, 0, len(tasks))
	channelIDs := make([]int, 0, len(channels))
	for id := range channels {
		channelIDs = append(channelIDs, id)
	}
	slices.Sort(channelIDs)
	for _, id := range channelIDs {
		for _, taskID := range channels[id] {
			if task := tasks[taskID]; task != nil {
				value := record.ToDomain(task)
				selected = append(selected, value)
			}
		}
	}
	store := &fixtureInventory{PollingStore: taskstore.PollingStore{Store: taskstore.NewStore(func() *gorm.DB { return model.DB })}, tasks: selected}
	selectedFactory := fixtureFactory{open: func(constant.TaskPlatform) TaskPollingAdaptor {
		if factory.open == nil {
			return nil
		}
		return factory.open(platform)
	}}
	u := fixtureUsecase(store, fixtureChannels{override: override}, selectedFactory, func() int { return 0 }, nil)
	_, err := u.Execute(ctx, nil)
	for _, value := range selected {
		if original := tasks[value.GetUpstreamTaskID()]; original != nil {
			*original = *record.FromDomain(value)
		}
	}
	return err
}

func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channels map[int][]string, tasks map[string]*model.Task) error {
	return runSelectedPolling(ctx, platform, fixtureFactory{open: GetTaskAdaptorFunc}, channels, tasks, nil)
}
func DispatchPlatformUpdate(ctx context.Context, platform constant.TaskPlatform, channels map[int][]string, tasks map[string]*model.Task) {
	_ = runSelectedPolling(ctx, platform, fixtureFactory{open: GetTaskAdaptorFunc}, channels, tasks, nil)
}
func UpdateBatchTasks(ctx context.Context, adaptor BatchTaskPollingAdaptor, channels map[int][]string, tasks map[string]*model.Task) error {
	return runSelectedPolling(ctx, "fixture", fixtureFactory{open: func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }}, channels, tasks, nil)
}
func updateBatchTasks(ctx context.Context, adaptor BatchTaskPollingAdaptor, channelID int, ids []string, tasks map[string]*model.Task) error {
	return UpdateBatchTasks(ctx, adaptor, map[int][]string{channelID: ids}, tasks)
}
func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, taskID string, tasks map[string]*model.Task) error {
	return runSelectedPolling(ctx, "fixture", fixtureFactory{open: func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }}, map[int][]string{ch.Id: {taskID}}, tasks, ch)
}
func sweepTimedOutTasks(ctx context.Context) {
	store := &fixtureInventory{PollingStore: taskstore.PollingStore{Store: taskstore.NewStore(func() *gorm.DB { return model.DB })}}
	u := fixtureUsecase(store, fixtureChannels{}, nil, func() int { return constant.TaskTimeoutMinutes }, nil)
	_, _ = u.Execute(ctx, nil)
}
func redactVideoResponseBody(body []byte) []byte { return (effects.Codec{}).RedactResponse(body) }

// failureFixtureAdaptor supplies a terminal provider observation to exercise the
// same public scenario instead of retaining its former private failure helper.
type failureFixtureAdaptor struct {
	TaskPollingAdaptor
	reason string
}

func (a failureFixtureAdaptor) FetchTask(string, string, *model.Task, string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}
func (a failureFixtureAdaptor) ParseTaskResult(*model.Task, *http.Response, []byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: a.reason}, nil
}

type observedCompletion struct {
	poll_tasks.Completion
	err error
}

func (c *observedCompletion) Settle(ctx context.Context, task *domain.Task, result *domain.TaskInfo, adjust func(*domain.Task, *domain.TaskInfo) int) (bool, error) {
	settled, err := c.Completion.Settle(ctx, task, result, adjust)
	c.err = err
	return settled, err
}
func failTaskFromPoll(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, fromStatus model.TaskStatus, reason string) error {
	selected := record.ToDomain(task)
	selected.Status = domain.TaskStatus(fromStatus)
	store := &fixtureInventory{PollingStore: taskstore.PollingStore{Store: taskstore.NewStore(func() *gorm.DB { return model.DB })}, tasks: []*domain.Task{selected}}
	completion := &observedCompletion{Completion: tasksapp.NewBilling()}
	factory := fixtureFactory{open: func(constant.TaskPlatform) TaskPollingAdaptor {
		return failureFixtureAdaptor{TaskPollingAdaptor: adaptor, reason: reason}
	}}
	u := fixtureUsecase(store, fixtureChannels{override: &model.Channel{Id: task.ChannelId}}, factory, func() int { return 0 }, completion)
	_, err := u.Execute(ctx, nil)
	*task = *record.FromDomain(selected)
	if completion.err != nil {
		return completion.err
	}
	return err
}
