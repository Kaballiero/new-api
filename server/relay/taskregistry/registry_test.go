package taskregistry_test

import (
	"testing"

	"github.com/QuantumNous/new-api/relay/taskregistry"

	"github.com/stretchr/testify/require"
)

func TestFactoryIsAvailableWithoutRootRelayOrMain(t *testing.T) {
	require.NotNil(t, taskregistry.GetTaskAdaptor("sora"))
}
