package taskregistry_test

import (
	"github.com/QuantumNous/new-api/relay/taskregistry"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFactoryIsAvailableWithoutRootRelayOrMain(t *testing.T) {
	require.NotNil(t, taskregistry.GetTaskAdaptor("sora"))
}
