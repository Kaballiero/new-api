// Package tasklog contains the narrowly approved task-contract text comparison.
package tasklog

import (
	"fmt"
	"slices"
	"strings"
)

// CanonicalContent accepts only permutations of the declared key/value list.
// The caller must supply the exact producer prefix and independently expected values.
func CanonicalContent(content, prefix string, parameters map[string]string) (string, error) {
	suffix, ok := strings.CutPrefix(content, prefix)
	if !ok || prefix == "" || len(parameters) == 0 {
		return "", fmt.Errorf("unexpected task billing content prefix or parameters")
	}
	entries := strings.Split(suffix, ", ")
	if len(entries) != len(parameters) {
		return "", fmt.Errorf("unexpected task billing parameter count")
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		key, value, found := strings.Cut(entry, ": ")
		expected, exists := parameters[key]
		if !found || !exists || seen[key] || value != expected {
			return "", fmt.Errorf("unexpected task billing parameter")
		}
		seen[key] = true
	}
	keys := make([]string, 0, len(parameters))
	for key, value := range parameters {
		if key == "" || strings.ContainsAny(key, ":,\r\n") || strings.Contains(value, ", ") {
			return "", fmt.Errorf("ambiguous expected task billing parameter")
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for i, key := range keys {
		entries[i] = key + ": " + parameters[key]
	}
	return prefix + strings.Join(entries, ", "), nil
}
