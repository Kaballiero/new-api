package tasks

import "github.com/QuantumNous/new-api/internal/tasks/record_task_submission"

// NewSubmission assembles the stateless accepted-task snapshot scenario.
func NewSubmission() record_task_submission.Usecase { return record_task_submission.NewUsecase() }
