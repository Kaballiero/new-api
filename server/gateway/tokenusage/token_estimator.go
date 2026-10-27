package tokenusage

import (
	"math"
	"strings"
	"sync"
	"unicode"
)

// Provider identifies the model provider family.
type Provider string

const (
	OpenAI Provider = "openai" // GPT-3.5, GPT-4 and GPT-4o.
	Gemini Provider = "gemini" // Gemini 1.0 and 1.5 Pro/Flash.
	Claude Provider = "claude" // Claude 3 and 3.5 Sonnet.
)

// multipliers holds provider-specific token estimation weights.
type multipliers struct {
	Word       float64 // English words, per word.
	Number     float64 // Numbers, per consecutive digit sequence.
	CJK        float64 // CJK characters, per character.
	Symbol     float64 // Punctuation, per character.
	MathSymbol float64 // Mathematical symbols, per character.
	URLDelim   float64 // URL separators (/,:,?,&,=,#,%), efficiently tokenized.
	AtSign     float64 // At signs split words and use a higher weight.
	Emoji      float64 // Emoji, per character.
	Newline    float64 // Newlines and tabs, per character.
	Space      float64 // Spaces, per character.
	BasePad    int     // Base padding for start and end tokens.
}

var (
	multipliersMap = map[Provider]multipliers{
		Gemini: {
			Word: 1.15, Number: 2.8, CJK: 0.68, Symbol: 0.38, MathSymbol: 1.05, URLDelim: 1.2, AtSign: 2.5, Emoji: 1.08, Newline: 1.15, Space: 0.2, BasePad: 0,
		},
		Claude: {
			Word: 1.13, Number: 1.63, CJK: 1.21, Symbol: 0.4, MathSymbol: 4.52, URLDelim: 1.26, AtSign: 2.82, Emoji: 2.6, Newline: 0.89, Space: 0.39, BasePad: 0,
		},
		OpenAI: {
			Word: 1.02, Number: 1.55, CJK: 0.85, Symbol: 0.4, MathSymbol: 2.68, URLDelim: 1.0, AtSign: 2.0, Emoji: 2.12, Newline: 0.5, Space: 0.42, BasePad: 0,
		},
	}
	multipliersLock sync.RWMutex
)

// getMultipliers returns the estimation weights for a provider.
func getMultipliers(p Provider) multipliers {
	multipliersLock.RLock()
	defer multipliersLock.RUnlock()

	switch p {
	case Gemini:
		return multipliersMap[Gemini]
	case Claude:
		return multipliersMap[Claude]
	case OpenAI:
		return multipliersMap[OpenAI]
	default:
		// Use OpenAI weights as the default.
		return multipliersMap[OpenAI]
	}
}

// EstimateToken estimates the token count.
func EstimateToken(provider Provider, text string) int {
	m := getMultipliers(provider)
	var count float64

	// Track word state.
	type WordType int
	const (
		None WordType = iota
		Latin
		Number
	)
	currentWordType := None

	for _, r := range text {
		// Handle spaces and newlines.
		if unicode.IsSpace(r) {
			currentWordType = None
			// Use the Newline weight for newlines and tabs.
			if r == '\n' || r == '\t' {
				count += m.Newline
			} else {
				// Use the Space weight for ordinary spaces.
				count += m.Space
			}
			continue
		}

		// Count CJK characters individually.
		if isCJK(r) {
			currentWordType = None
			count += m.CJK
			continue
		}

		// Use the Emoji weight for emoji.
		if isEmoji(r) {
			currentWordType = None
			count += m.Emoji
			continue
		}

		// Handle Latin letters and digits as words.
		if isLatinOrNumber(r) {
			isNum := unicode.IsNumber(r)
			newType := Latin
			if isNum {
				newType = Number
			}

			// Start a token at word boundaries and transitions between letters and digits.
			// OpenAI typically splits "version 3.5" and may also split "abc123xyz".
			// Approximate these splits by adding weight at letter/digit transitions.
			if currentWordType == None || currentWordType != newType {
				if newType == Number {
					count += m.Number
				} else {
					count += m.Word
				}
				currentWordType = newType
			}
			// Do not add weight for the remaining word characters.
			continue
		}

		// Apply the corresponding weight to punctuation and special characters.
		currentWordType = None
		if isMathSymbol(r) {
			count += m.MathSymbol
		} else if r == '@' {
			count += m.AtSign
		} else if isURLDelim(r) {
			count += m.URLDelim
		} else {
			count += m.Symbol
		}
	}

	// Round up and add base padding.
	return int(math.Ceil(count)) + m.BasePad
}

// Recognize CJK characters.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		(r >= 0x3040 && r <= 0x30FF) || // Japanese.
		(r >= 0xAC00 && r <= 0xD7A3) // Korean.
}

// Recognize word characters: letters and digits.
func isLatinOrNumber(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

// Recognize emoji characters.
func isEmoji(r rune) bool {
	// Emoji Unicode ranges.
	// Primary range: 0x1F300-0x1F9FF (emoticons, symbols and pictographs).
	// Additional ranges: 0x2600-0x26FF (miscellaneous symbols), 0x2700-0x27BF (dingbats).
	// Emoticons: 0x1F600-0x1F64F.
	// Supplemental symbols and pictographs: 0x1F900-0x1F9FF.
	return (r >= 0x1F300 && r <= 0x1F9FF) ||
		(r >= 0x2600 && r <= 0x26FF) ||
		(r >= 0x2700 && r <= 0x27BF) ||
		(r >= 0x1F600 && r <= 0x1F64F) ||
		(r >= 0x1F900 && r <= 0x1F9FF) ||
		(r >= 0x1FA00 && r <= 0x1FAFF) // Symbols and Pictographs Extended-A
}

// Recognize mathematical symbols.
func isMathSymbol(r rune) bool {
	// Mathematical operators and symbols.
	// Basic mathematical symbols: ∑ ∫ ∂ √ ∞ ≤ ≥ ≠ ≈ ± × ÷.
	// Superscript and subscript digits: ² ³ ¹ ⁴ ⁵ ⁶ ⁷ ⁸ ⁹ ⁰.
	// Greek letters are also commonly used in mathematics.
	mathSymbols := "∑∫∂√∞≤≥≠≈±×÷∈∉∋∌⊂⊃⊆⊇∪∩∧∨¬∀∃∄∅∆∇∝∟∠∡∢°′″‴⁺⁻⁼⁽⁾ⁿ₀₁₂₃₄₅₆₇₈₉₊₋₌₍₎²³¹⁴⁵⁶⁷⁸⁹⁰"
	for _, m := range mathSymbols {
		if r == m {
			return true
		}
	}
	// Mathematical Operators (U+2200–U+22FF)
	if r >= 0x2200 && r <= 0x22FF {
		return true
	}
	// Supplemental Mathematical Operators (U+2A00–U+2AFF)
	if r >= 0x2A00 && r <= 0x2AFF {
		return true
	}
	// Mathematical Alphanumeric Symbols (U+1D400–U+1D7FF)
	if r >= 0x1D400 && r <= 0x1D7FF {
		return true
	}
	return false
}

// Recognize URL separators that tokenizers handle efficiently.
func isURLDelim(r rune) bool {
	// Common URL separators usually receive optimized tokenization.
	urlDelims := "/:?&=;#%"
	for _, d := range urlDelims {
		if r == d {
			return true
		}
	}
	return false
}

func EstimateTokenByModel(model, text string) int {
	// strings.Contains(model, "gpt-4o")
	if text == "" {
		return 0
	}

	model = strings.ToLower(model)
	if strings.Contains(model, "gemini") {
		return EstimateToken(Gemini, text)
	} else if strings.Contains(model, "claude") {
		return EstimateToken(Claude, text)
	} else {
		return EstimateToken(OpenAI, text)
	}
}
