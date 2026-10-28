package usage

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
)

func projectLogs(logs []*record.Log, policy domain.VisibilityPolicy, start int) {
	for i, item := range logs {
		item.Other = record.FormatLogOtherJSON(item.Other, policy)
		if policy.HideChannelNames {
			item.ChannelName = ""
		}
		if policy.ReplaceDisplayIDs {
			item.Id = start + i + 1
		}
	}
}

func logDTOs(logs []*record.Log, policy domain.VisibilityPolicy, start int) []*dto.Log {
	projectLogs(logs, policy, start)
	if logs == nil {
		return nil
	}
	out := make([]*dto.Log, len(logs))
	for i, row := range logs {
		value := dto.Log(*row)
		out[i] = &value
	}
	return out
}
