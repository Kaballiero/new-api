package contracttest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeStripeClientUserAgent(t *testing.T) {
	for _, tc := range []struct {
		name, input, expected string
	}{
		{
			name:     "SDK metadata",
			input:    `{"application":null,"bindings_version":"81.4.0","lang":"go","lang_version":"go1.27.1","publisher":"stripe","uname":"Linux fixture 6.8.0\n"}`,
			expected: `{"application":null,"bindings_version":"81.4.0","lang":"go","lang_version":"\u003cgo-version\u003e","publisher":"stripe","uname":"\u003chost-uname\u003e"}`,
		},
		{
			name:     "field order and application data",
			input:    `{"uname":"Darwin fixture","application":{"name":"app","uname":"keep","version":"1.2"},"lang_version":"go1.25.1","extra":[1,false,"go1.27"],"bindings_version":"81.4.0","lang":"go","publisher":"stripe"}`,
			expected: `{"uname":"\u003chost-uname\u003e","application":{"name":"app","uname":"keep","version":"1.2"},"lang_version":"\u003cgo-version\u003e","extra":[1,false,"go1.27"],"bindings_version":"81.4.0","lang":"go","publisher":"stripe"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual := NormalizeStripeClientUserAgent(t, tc.input)
			assert.Equal(t, tc.expected, actual)
			assert.Equal(t, actual, NormalizeStripeClientUserAgent(t, actual))
		})
	}
}
