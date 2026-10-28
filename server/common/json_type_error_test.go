package common_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/deployments/create_deployment"
	"github.com/QuantumNous/new-api/pkg/ionet"

	"github.com/stretchr/testify/require"
)

func TestPreserveMovedJSONDecoderTypeNames(t *testing.T) {
	for _, body := range []string{`[]`, `{"container_config":true}`, `{"registry_config":1}`, `{"hardware_id":"bad"}`} {
		t.Run(body, func(t *testing.T) {
			var old ionet.DeploymentRequest
			var moved create_deployment.DeploymentRequest
			original := common.Unmarshal([]byte(body), &old)
			actual := common.Unmarshal([]byte(body), &moved)
			require.Error(t, original)
			require.Error(t, actual)
			stable := common.PreserveJSONTypeError(actual, "github.com/QuantumNous/new-api/internal/deployments/", "ionet")
			require.EqualError(t, stable, original.Error())
			var typed *json.UnmarshalTypeError
			require.True(t, errors.As(stable, &typed))
			require.NotNil(t, typed)
			require.Same(t, actual, common.PreserveJSONTypeError(actual, "unrelated/", "ionet"))
		})
	}
	require.NoError(t, common.PreserveJSONTypeError(nil, "owned/", "ionet"))
	sentinel := errors.New("transport error")
	require.Same(t, sentinel, common.PreserveJSONTypeError(sentinel, "owned/", "ionet"))
}
