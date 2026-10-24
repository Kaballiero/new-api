package tasklog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalContent(t *testing.T) {
	const prefix = "操作 text_to_video, 计算参数："
	parameters := map[string]string{"seconds": "6", "resolution": "768P"}
	for _, suffix := range []string{"seconds: 6, resolution: 768P", "resolution: 768P, seconds: 6"} {
		actual, err := CanonicalContent(prefix+suffix, prefix, parameters)
		require.NoError(t, err)
		assert.Equal(t, prefix+"resolution: 768P, seconds: 6", actual)
	}
	for _, input := range []string{
		prefix + "seconds: 7, resolution: 768P", prefix + "seconds: 6", prefix + "seconds: 6, resolution: 768P, extra: 1",
		"changed prefix seconds: 6, resolution: 768P", prefix + "seconds: 6; resolution: 768P",
		prefix + "seconds: 6,resolution: 768P", prefix + "seconds: 6, seconds: 6", prefix + "seconds:6, resolution: 768P",
	} {
		_, err := CanonicalContent(input, prefix, parameters)
		assert.Error(t, err, input)
	}
}
