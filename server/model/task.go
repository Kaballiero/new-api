package model

import "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"

type TaskStatus = record.TaskStatus

const TaskStatusNotStart = record.TaskStatusNotStart

const TaskStatusSubmitted = record.TaskStatusSubmitted

const TaskStatusQueued = record.TaskStatusQueued

const TaskStatusInProgress = record.TaskStatusInProgress

const TaskStatusFailure = record.TaskStatusFailure

const TaskStatusSuccess = record.TaskStatusSuccess

const TaskStatusUnknown = record.TaskStatusUnknown

const TaskRefundLegacyCutoff = record.TaskRefundLegacyCutoff

type Task = record.Task

type Properties = record.Properties

type TaskPrivateData = record.TaskPrivateData

type OpenRouterSyncTask = record.OpenRouterSyncTask

type OpenRouterAudioTask = record.OpenRouterAudioTask

type TaskReconciliation = record.TaskReconciliation

type TaskExecutionSnapshot = record.TaskExecutionSnapshot

type TaskPluginSnapshot = record.TaskPluginSnapshot

type TaskPluginAuthorSnapshot = record.TaskPluginAuthorSnapshot

const TaskSettlementOpenRouterCostV1 = record.TaskSettlementOpenRouterCostV1

const TaskSettlementOpenRouterCostV2 = record.TaskSettlementOpenRouterCostV2

type TaskBillingContext = record.TaskBillingContext

type TaskBillingFX = record.TaskBillingFX

type TaskBillingSubmitGroup = record.TaskBillingSubmitGroup

type SyncTaskQueryParams = record.SyncTaskQueryParams
