package domain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Lookup and encode failures stay raw so that the transport writes them as a
// plain {success:false} response, not as 500 internal_error.
func TestAutoGroupRulesReturnsRawDependencyErrors(t *testing.T) {
	lookup, encode := errors.New("user group lookup failed"), errors.New("encode failed")
	rules := AutoGroupRules{Max: func() int { return 2 }, UserGroup: func() (string, error) { return "", lookup }, Selectable: func(string, string) bool { return true }, Encode: func(*Token, []string) error { return encode }}

	err := rules.Apply(&Token{}, []string{"default"})
	require.Same(t, lookup, err)
	assert.NotErrorIs(t, err, ErrInternal)

	rules.UserGroup = func() (string, error) { return "default", nil }
	require.Same(t, encode, rules.Apply(&Token{}, []string{"default"}))
	require.Same(t, encode, rules.Apply(&Token{}, nil), "clearing groups also encodes")
}
