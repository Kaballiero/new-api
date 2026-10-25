package dto

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/batch/domain"
)

// Job is the public projection; persistence has no response codec.
type Job domain.Job

func (job Job) MarshalJSON() ([]byte, error) {
	type batchJob Job
	type publicFailure struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	var failure *publicFailure
	if job.Status == "failed" {
		failure = &publicFailure{Code: "batch_failed", Message: "The Batch operation failed."}
		var diagnostic domain.Failure
		if len(job.LastError) <= 4096 && common.UnmarshalJsonStr(job.LastError, &diagnostic) == nil &&
			diagnostic.Version == 1 && diagnostic.Phase == "create" && diagnostic.HTTPStatus >= 400 && diagnostic.HTTPStatus < 500 {
			failure.Code = "batch_creation_rejected"
			failure.Message = "The provider rejected Batch creation."
		}
	}
	return common.Marshal(struct {
		batchJob
		Error *publicFailure `json:"error,omitempty"`
	}{batchJob: batchJob(job), Error: failure})
}
