package openrouter

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
)

// decisionSearchQuestionLimit bounds the web searches of a decisions request:
// a decision model may search once per question, at most this many times.
const decisionSearchQuestionLimit = 100

// decisionQuantities measures a decisions request by its state and questions:
// the prompt tokens they count as and the web searches the model may run, one
// per question up to decisionSearchQuestionLimit. Every UTF-8 byte counts as
// a token, the upper bound of a byte-level BPE tokenizer, which leaves room
// for the prompt template a provider wraps them in. The request validator
// guarantees that questions is a nonempty object.
func decisionQuantities(fields map[string]json.RawMessage) (promptTokens, searches int) {
	var questions map[string]json.RawMessage
	_ = common.Unmarshal(fields["questions"], &questions)
	return len(fields["state"]) + len(fields["questions"]), min(len(questions), decisionSearchQuestionLimit)
}
