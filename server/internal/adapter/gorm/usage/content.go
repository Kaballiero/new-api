package usage

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"strings"
)

type Content struct{ Quota func(int) string }

// RenderNote renders a selected business note; it does not select recording policy.
func (f Content) RenderNote(note domain.Note) string {
	switch note.Kind {
	case domain.LiteralContent:
		return note.Name
	case domain.MissingUpstreamUsage:
		return "上游无计费信息"
	case domain.ToolCharge:
		return fmt.Sprintf("%s 调用 %d 次，调用花费 %s", note.Name, note.Count, f.Quota(note.Quota))
	case domain.AudioInputCharge:
		return fmt.Sprintf("Audio Input 花费 %s", f.Quota(note.Quota))
	case domain.UnbillableUsage:
		return "上游没有返回计费信息，无法扣费（可能是上游超时）"
	case domain.SettlementRepair:
		return "OpenRouter settlement requires manual repair"
	case domain.OriginalModel:
		return fmt.Sprintf("模型 %s", note.Name)
	case domain.AudioRatios:
		return fmt.Sprintf(
			"模型倍率 %.2f，补全倍率 %.2f，音频倍率 %.2f，音频补全倍率 %.2f，分组倍率 %.2f",
			note.Prices.ModelRatio, note.Prices.CompletionRatio,
			note.AudioRatio, note.AudioCompletionRatio, note.Prices.GroupRatio,
		)
	case domain.ModelPrice:
		return fmt.Sprintf("模型价格 %.2f，分组倍率 %.2f", note.Prices.ModelPrice, note.Prices.GroupRatio)
	case domain.PossibleTimeout:
		return "（可能是上游超时）"
	case domain.DeliveryFailed:
		return "response delivery failed: " + note.Name
	default:
		return ""
	}
}

// RenderContent is the sole owner of persisted note punctuation and joining.
func (f Content) RenderContent(notes []domain.Note) string {
	var content strings.Builder
	for i, note := range notes {
		if i > 0 && note.Join != domain.Adjacent {
			content.WriteString(", ")
		}
		content.WriteString(f.RenderNote(note))
	}
	return content.String()
}
