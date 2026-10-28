package dto

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/batch/domain"
)

// Job is the public projection; persistence has no response codec.
type Job domain.Job

// LocalizedJob carries response language without changing the durable Job record.
type LocalizedJob struct {
	job      Job
	language string
}

func (job Job) WithLanguage(language string) LocalizedJob { return LocalizedJob{job, language} }
func (job Job) MarshalJSON() ([]byte, error)              { return job.WithLanguage(i18n.DefaultLang).MarshalJSON() }
func (localized LocalizedJob) MarshalJSON() ([]byte, error) {
	job := localized.job
	type batchJob Job
	type publicFailure struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	var failure *publicFailure
	if job.Status == "failed" {
		failure = &publicFailure{Code: "batch_failed", Message: i18n.Translate(localized.language, i18n.MsgBatchFailed)}
		var diagnostic domain.Failure
		if len(job.LastError) <= 4096 && common.UnmarshalJsonStr(job.LastError, &diagnostic) == nil &&
			diagnostic.Version == 1 && diagnostic.Phase == "create" && diagnostic.HTTPStatus >= 400 && diagnostic.HTTPStatus < 500 {
			failure.Code = "batch_creation_rejected"
			failure.Message = i18n.Translate(localized.language, i18n.MsgBatchCreationRejected)
		}
	}
	return common.Marshal(struct {
		batchJob
		Error *publicFailure `json:"error,omitempty"`
	}{batchJob: batchJob(job), Error: failure})
}
