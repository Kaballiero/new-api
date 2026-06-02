package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
)

func TestGetModelSupportEndpointTypes(t *testing.T) {
	modelSupportEndpointsLock.Lock()
	modelSupportEndpointTypes["test-image-model"] = []constant.EndpointType{
		constant.EndpointTypeImageGeneration,
		constant.EndpointTypeOpenAI,
	}
	modelSupportEndpointsLock.Unlock()
	t.Cleanup(func() {
		modelSupportEndpointsLock.Lock()
		delete(modelSupportEndpointTypes, "test-image-model")
		modelSupportEndpointsLock.Unlock()
	})

	if got := GetModelSupportEndpointTypes(""); len(got) != 0 {
		t.Errorf("empty model: got %v, want empty", got)
	}
	if got := GetModelSupportEndpointTypes("unknown-model-xyz"); len(got) != 0 {
		t.Errorf("unknown model: got %v, want empty", got)
	}
	got := GetModelSupportEndpointTypes("test-image-model")
	if len(got) != 2 || got[0] != constant.EndpointTypeImageGeneration {
		t.Errorf("registered model: got %v, want [image-generation, openai] (first must be image-generation)", got)
	}
}
