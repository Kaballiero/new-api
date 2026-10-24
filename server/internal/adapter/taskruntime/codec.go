package taskruntime

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Codec retains the provider and stored JSON behavior of the legacy poller.
type Codec struct{}

func (Codec) Marshal(value any) ([]byte, error)      { return common.Marshal(value) }
func (Codec) Unmarshal(data []byte, value any) error { return common.Unmarshal(data, value) }

func (Codec) ParseGatewayResponse(body []byte) (*domain.TaskInfo, []byte, any, bool) {
	var response dto.TaskResponse[record.Task]
	if err := common.Unmarshal(body, &response); err != nil || !response.IsSuccess() {
		return nil, nil, nil, false
	}
	task := response.Data
	return &domain.TaskInfo{TaskID: task.TaskID, Status: string(task.Status), Url: task.GetResultURL(),
		Progress: task.Progress, Reason: task.FailReason}, task.Data, response, true
}

func (Codec) RedactResponse(body []byte) []byte {
	var value map[string]any
	if err := common.Unmarshal(body, &value); err != nil {
		return body
	}
	response, _ := value["response"].(map[string]any)
	if response != nil {
		delete(response, "bytesBase64Encoded")
		if video, ok := response["video"].(string); ok {
			if len(video) > 256 {
				video = video[:256] + "..."
			}
			response["video"] = video
		}
		if videos, ok := response["videos"].([]any); ok {
			for i := range videos {
				if item, ok := videos[i].(map[string]any); ok {
					delete(item, "bytesBase64Encoded")
				}
			}
		}
	}
	encoded, err := common.Marshal(value)
	if err != nil {
		return body
	}
	return encoded
}
