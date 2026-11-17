package openrouter

import (
	"encoding/json"
	"errors"

	"github.com/QuantumNous/new-api/common"
	tokenusage "github.com/QuantumNous/new-api/gateway/tokenusage"
)

// decisionSearchQuestionLimit bounds the web searches of a decisions request:
// a decision model may search once per question, at most this many times.
const decisionSearchQuestionLimit = 100

// decisionQuantities measures a decisions request by its state and questions:
// the prompt tokens they count as and the web searches the model may run, one
// per question up to decisionSearchQuestionLimit. An OpenAI text model counts
// the tokens with its tokenizer. Any other model counts every UTF-8 byte as a
// token, the upper bound of a byte-level BPE tokenizer: the word heuristic the
// gateway estimates other models with undercounts long tokens.
func decisionQuantities(fields map[string]json.RawMessage, model string) (promptTokens, searches int, err error) {
	var questions map[string]json.RawMessage
	if common.Unmarshal(fields["questions"], &questions) != nil {
		return 0, 0, errors.New("invalid OpenRouter decisions questions")
	}
	text := string(fields["state"]) + string(fields["questions"])
	promptTokens = len(text)
	if common.IsOpenAITextModel(model) {
		promptTokens = tokenusage.CountTextToken(text, model)
	}
	return promptTokens, min(len(questions), decisionSearchQuestionLimit), nil
}
