package service

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
)

func TestMain(m *testing.M) { taskfixture.Main(m) }
func truncate(t *testing.T) { taskfixture.Truncate(t) }
