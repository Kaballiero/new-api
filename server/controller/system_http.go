package controller

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/app/systemruntime"
)

var systemHTTP = sync.OnceValue(systemruntime.NewHTTP)
