package taskmultipart

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func soraParts() []Part {
	return []Part{
		{Name: "model", Headers: "Content-Disposition: form-data; name=model", Data: []byte("sora-2")},
		{Name: "prompt", Headers: "Content-Disposition: form-data; name=prompt", Data: []byte("fixture")},
		{Name: "seconds", Headers: "Content-Disposition: form-data; name=seconds", Data: []byte("4")},
		{Name: "input_reference", Filename: "fixture.png",
			Headers: "Content-Disposition: form-data; filename=fixture.png; name=input_reference\r\nContent-Type: application/octet-stream",
			Data:    []byte("fixture-image")},
	}
}

func multipartBody(parts []Part) []byte {
	var body bytes.Buffer
	for _, part := range parts {
		body.WriteString("--" + strings.Repeat("a", 60) + "\r\n" + part.Headers + "\r\n\r\n")
		body.Write(part.Data)
		body.WriteString("\r\n")
	}
	body.WriteString("--" + strings.Repeat("a", 60) + "--\r\n")
	return body.Bytes()
}

func TestCanonicalBody(t *testing.T) {
	expected := soraParts()
	canonical, err := CanonicalBody(multipartBody([]Part{expected[3], expected[0], expected[1], expected[2]}), expected)
	require.NoError(t, err)
	for _, order := range [][]int{{0, 1, 2, 3}, {2, 0, 1, 3}, {1, 3, 2, 0}} {
		parts := make([]Part, 0, len(order))
		for _, index := range order {
			parts = append(parts, expected[index])
		}
		actual, err := CanonicalBody(multipartBody(parts), expected)
		require.NoError(t, err)
		assert.Equal(t, canonical, actual)
	}
	assert.Equal(t, bytes.ReplaceAll(multipartBody([]Part{expected[3], expected[0], expected[1], expected[2]}),
		[]byte(strings.Repeat("a", 60)), []byte("<multipart-boundary>")), canonical)
}

func TestCanonicalBodyRejectsChanges(t *testing.T) {
	original := multipartBody(soraParts())
	extra := append(soraParts(), Part{Name: "extra", Headers: "Content-Disposition: form-data; name=extra", Data: []byte("x")})
	duplicate := append(soraParts()[:3], soraParts()[0])
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"value", bytes.Replace(original, []byte("\r\n4\r\n"), []byte("\r\n5\r\n"), 1)},
		{"missing", multipartBody(soraParts()[:3])},
		{"extra", multipartBody(extra)},
		{"duplicate", multipartBody(duplicate)},
		{"header", bytes.Replace(original, []byte("application/octet-stream"), []byte("image/png"), 1)},
		{"header_order", bytes.Replace(original,
			[]byte(soraParts()[3].Headers),
			[]byte("Content-Type: application/octet-stream\r\nContent-Disposition: form-data; filename=fixture.png; name=input_reference"), 1)},
		{"filename", bytes.Replace(original, []byte("filename=fixture.png"), []byte("filename=changed.png"), 1)},
		{"file_bytes", bytes.Replace(original, []byte("fixture-image"), []byte("changed-image"), 1)},
		{"preamble", append([]byte("preamble\r\n"), original...)},
		{"epilogue", append(bytes.Clone(original), []byte("epilogue\r\n")...)},
		{"separator", bytes.Replace(original, []byte("\r\n\r\n"), []byte("\n\n"), 1)},
		{"line_endings", bytes.ReplaceAll(original, []byte("\r\n"), []byte("\n"))},
		{"trailing_newline", bytes.TrimSuffix(original, []byte("\r\n"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalBody(tc.body, soraParts())
			assert.Error(t, err)
		})
	}
}

func TestCanonicalBodySortsFilename(t *testing.T) {
	parts := []Part{
		{Name: "reference", Filename: "z.png", Headers: "Content-Disposition: form-data; filename=z.png; name=reference", Data: []byte("z")},
		{Name: "reference", Filename: "a.png", Headers: "Content-Disposition: form-data; filename=a.png; name=reference", Data: []byte("a")},
	}
	actual, err := CanonicalBody(multipartBody(parts), parts)
	require.NoError(t, err)
	expected := bytes.ReplaceAll(multipartBody([]Part{parts[1], parts[0]}),
		[]byte(strings.Repeat("a", 60)), []byte("<multipart-boundary>"))
	assert.Equal(t, expected, actual)
}
