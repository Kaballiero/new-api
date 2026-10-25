package model

import "github.com/QuantumNous/new-api/pkg/batchstate"

// BRIDGE: newHardDeletionOperation returns the shared Batch conflict sentinel;
// remove this legacy name when wave 7 moves its Batch financial-decision port.
var ErrBatchConflict = batchstate.ErrConflict
