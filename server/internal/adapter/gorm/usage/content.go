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
		return "No upstream billing information"
	case domain.ToolCharge:
		return fmt.Sprintf("%s called %d times, cost %s", note.Name, note.Count, f.Quota(note.Quota))
	case domain.AudioInputCharge:
		return fmt.Sprintf("Audio input cost %s", f.Quota(note.Quota))
	case domain.UnbillableUsage:
		return "Upstream returned no billing information; unable to charge (possible upstream timeout)"
	case domain.SettlementRepair:
		return "OpenRouter settlement requires manual repair"
	case domain.OriginalModel:
		return fmt.Sprintf("Model %s", note.Name)
	case domain.AudioRatios:
		return fmt.Sprintf(
			"Model ratio %.2f, completion ratio %.2f, audio ratio %.2f, audio completion ratio %.2f, group ratio %.2f",
			note.Prices.ModelRatio, note.Prices.CompletionRatio,
			note.AudioRatio, note.AudioCompletionRatio, note.Prices.GroupRatio,
		)
	case domain.ModelPrice:
		return fmt.Sprintf("Model price %.2f, group ratio %.2f", note.Prices.ModelPrice, note.Prices.GroupRatio)
	case domain.PossibleTimeout:
		return " (possible upstream timeout)"
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
