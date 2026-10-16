package reasoning

import (
	"fmt"
	"strings"
)

// SupportsExecutionMode covers the documented GPT-5.6/GPT-6 families. Older
// Pro model IDs have a separate contract and must not be inferred from a suffix.
func SupportsExecutionMode(model string) bool {
	model = strings.TrimPrefix(model, "openai/")
	for _, base := range []string{"gpt-5.6", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-6.1-sol"} {
		if model == base || model == base+"-pro" {
			return true
		}
		for _, prefix := range []string{base + "-", base + "-pro-"} {
			suffix, ok := strings.CutPrefix(model, prefix)
			if ok && len(suffix) == 10 && suffix[4] == '-' && suffix[7] == '-' {
				valid := true
				for i, c := range suffix {
					if i != 4 && i != 7 && (c < '0' || c > '9') {
						valid = false
					}
				}
				if valid {
					return true
				}
			}
		}
	}
	return false
}

// OpenRouterExecutionModel selects metadata for the model OpenRouter actually
// executes. This affects reservation only; the request remains base + mode.
func OpenRouterExecutionModel(model, mode string) (string, error) {
	if mode == "" || mode == "standard" {
		return model, nil
	}
	if mode != "pro" || !strings.HasPrefix(model, "openai/") || !SupportsExecutionMode(model) {
		return "", fmt.Errorf("unsupported OpenRouter execution mode/model")
	}
	if strings.Contains(model, "-pro") {
		return model, nil
	}
	// Dated variants need an explicit catalog identity rather than guessing where
	// the Pro suffix belongs. Keep these fail closed until metadata proves it.
	for _, base := range []string{"openai/gpt-5.6", "openai/gpt-5.6-terra", "openai/gpt-5.6-luna", "openai/gpt-6-astra", "openai/gpt-6-sol", "openai/gpt-6-luna", "openai/gpt-6.1-sol"} {
		if model == base {
			return model + "-pro", nil
		}
	}
	return "", fmt.Errorf("OpenRouter Pro metadata identity is unproved for %s", model)
}
