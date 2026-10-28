// Package batchstate defines the conflict sentinel shared by Batch and legacy account deletion.
package batchstate

import "errors"

var ErrConflict = errors.New("batch state conflict")
