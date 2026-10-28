package domain

import "strings"

// SanitizeSearchPattern escapes a user LIKE pattern for ESCAPE '!' and checks it
// with ValidateSearchPattern. A pattern without % matches the whole value.
func SanitizeSearchPattern(input string) (string, error) {
	// Escape the ESCAPE character ! itself first, then _. ! is used instead of \
	// to avoid backslash string escaping in MySQL.
	input = strings.ReplaceAll(input, "!", "!!")
	input = strings.ReplaceAll(input, `_`, `!_`)

	if err := ValidateSearchPattern(input); err != nil {
		return "", err
	}

	return input, nil
}

// ValidateSearchPattern rejects consecutive %, more than two % and fuzzy
// keywords shorter than two characters.
func ValidateSearchPattern(input string) error {
	// Consecutive % are rejected.
	if strings.Contains(input, "%%") {
		return ErrSearchConsecutiveWildcards
	}

	// At most two %.
	count := strings.Count(input, "%")
	if count > 2 {
		return ErrSearchTooManyWildcards
	}

	// A fuzzy keyword without % must have at least two characters.
	if count > 0 {
		stripped := strings.ReplaceAll(input, "%", "")
		if len(stripped) < 2 {
			return ErrSearchKeywordTooShort
		}
	}

	return nil
}
