package taskplugins

import (
	"net/http"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/poll_tasks"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/taskregistry"
)

type PollingFactory struct{}

func (PollingFactory) Open(platform string) poll_tasks.Adaptor {
	adaptor := taskregistry.GetTaskAdaptor(constant.TaskPlatform(platform))
	if adaptor == nil {
		return nil
	}
	p := poller{adaptor: adaptor}
	if batch, ok := adaptor.(batchGateway); ok {
		return batchPoller{poller: p, batch: batch}
	}
	return p
}

type poller struct{ adaptor channel.TaskAdaptor }

type batchGateway interface {
	FetchMode() string
	FetchBatchTasks(baseURL, key string, tasks []*record.Task, proxy string) (*http.Response, error)
	ParseBatchResult(tasks []*record.Task, response *http.Response, body []byte) (map[string]*channel.BatchTaskResult, error)
}

type batchPoller struct {
	poller
	batch batchGateway
}

func (p poller) Init(baseURL, key string) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: baseURL}}
	info.ApiKey = key
	p.adaptor.Init(info)
}

func (p poller) FetchTask(baseURL, key string, task *domain.Task, proxy string) (*poll_tasks.Response, error) {
	response, err := p.adaptor.FetchTask(baseURL, key, record.FromDomain(task), proxy)
	if err != nil {
		return nil, err
	}
	return &poll_tasks.Response{StatusCode: response.StatusCode, Headers: response.Header, Body: response.Body}, nil
}

func (p poller) ParseTaskResult(task *domain.Task, response *poll_tasks.Response, body []byte) (*domain.TaskInfo, error) {
	info, err := p.adaptor.ParseTaskResult(record.FromDomain(task),
		&http.Response{StatusCode: response.StatusCode, Header: response.Headers, Body: response.Body}, body)
	if err != nil {
		return nil, err
	}
	return TaskInfoToDomain(info), nil
}

func (p poller) AdjustBillingOnComplete(task *domain.Task, result *domain.TaskInfo) int {
	row, info := record.FromDomain(task), TaskInfoFromDomain(result)
	quota := p.adaptor.AdjustBillingOnComplete(row, info)
	record.RestoreDomain(task, row)
	*result = *TaskInfoToDomain(info)
	return quota
}

func (p batchPoller) FetchMode() string { return p.batch.FetchMode() }

func (p batchPoller) FetchBatchTasks(baseURL, key string, tasks []*domain.Task, proxy string) (*poll_tasks.Response, error) {
	rows := make([]*record.Task, len(tasks))
	for i := range tasks {
		rows[i] = record.FromDomain(tasks[i])
	}
	response, err := p.batch.FetchBatchTasks(baseURL, key, rows, proxy)
	if err != nil {
		return nil, err
	}
	return &poll_tasks.Response{StatusCode: response.StatusCode, Headers: response.Header, Body: response.Body}, nil
}

func (p batchPoller) ParseBatchResult(tasks []*domain.Task, response *poll_tasks.Response, body []byte) (map[string]*poll_tasks.BatchResult, error) {
	rows := make([]*record.Task, len(tasks))
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
	for key, value := range results {
		if value == nil {
			mapped[key] = nil
			continue
		}
		mapped[key] = &poll_tasks.BatchResult{TaskInfo: *TaskInfoToDomain(&value.TaskInfo),
			SubmitTime: value.SubmitTime, StartTime: value.StartTime, FinishTime: value.FinishTime, Data: value.Data}
	}
	return mapped, nil
}

func TaskInfoToDomain(info *relaycommon.TaskInfo) *domain.TaskInfo {
	if info == nil {
		return nil
	}
	return &domain.TaskInfo{UpstreamCostUSD: info.UpstreamCostUSD, UpstreamCostConfirmed: info.UpstreamCostConfirmed,
		Code: info.Code, TaskID: info.TaskID, Status: info.Status, Reason: info.Reason, Url: info.Url, RemoteUrl: info.RemoteUrl,
		Progress: info.Progress, CompletionTokens: info.CompletionTokens, TotalTokens: info.TotalTokens,
		UsageFacts: info.UsageFacts, PluginState: info.PluginState}
}

func TaskInfoFromDomain(info *domain.TaskInfo) *relaycommon.TaskInfo {
	if info == nil {
		return nil
	}
	return &relaycommon.TaskInfo{UpstreamCostUSD: info.UpstreamCostUSD, UpstreamCostConfirmed: info.UpstreamCostConfirmed,
		Code: info.Code, TaskID: info.TaskID, Status: info.Status, Reason: info.Reason, Url: info.Url, RemoteUrl: info.RemoteUrl,
		Progress: info.Progress, CompletionTokens: info.CompletionTokens, TotalTokens: info.TotalTokens,
		UsageFacts: info.UsageFacts, PluginState: info.PluginState}
}
