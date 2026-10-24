// Package taskmultipart validates the exact legacy Sora multipart contract before ordering its parts.
package taskmultipart

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"mime"
	"net/textproto"
	"regexp"
	"slices"
)

// Part declares independently expected identity, raw headers and payload bytes.
type Part struct {
	Name, Filename, Headers string
	Data                    []byte
}

// CanonicalBody accepts only permutations of the declared complete parts.
// It preserves every part byte and validates the exact envelope before replacing delimiter boundaries.
func CanonicalBody(raw []byte, expected []Part) ([]byte, error) {
	first, _, found := bytes.Cut(raw, []byte("\r\n"))
	if !found || !regexp.MustCompile(`^--[0-9a-f]{60}$`).Match(first) || len(expected) == 0 {
		return nil, fmt.Errorf("unexpected legacy Sora multipart boundary")
	}
	segments := bytes.Split(raw, first)
	if len(segments) != len(expected)+2 || len(segments[0]) != 0 || !bytes.Equal(segments[len(segments)-1], []byte("--\r\n")) {
		return nil, fmt.Errorf("unexpected multipart envelope or part count")
	}
	type identity struct{ name, filename string }
	declared := make(map[identity]Part, len(expected))
	for _, part := range expected {
		key := identity{part.Name, part.Filename}
		if _, duplicate := declared[key]; part.Name == "" || duplicate {
			return nil, fmt.Errorf("invalid expected multipart part set")
		}
		declared[key] = part
	}
	type captured struct {
		key identity
		raw []byte
	}
	parts := make([]captured, 0, len(expected))
	seen := make(map[identity]bool, len(expected))
	for _, segment := range segments[1 : len(segments)-1] {
		if !bytes.HasPrefix(segment, []byte("\r\n")) || !bytes.HasSuffix(segment, []byte("\r\n")) || len(segment) < 4 {
			return nil, fmt.Errorf("unexpected multipart separator")
		}
		headers, data, found := bytes.Cut(segment[2:len(segment)-2], []byte("\r\n\r\n"))
		if !found {
			return nil, fmt.Errorf("missing multipart header separator")
		}
		reader := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(bytes.Clone(headers), '\r', '\n', '\r', '\n'))))
		parsed, err := reader.ReadMIMEHeader()
		if err != nil {
			return nil, fmt.Errorf("invalid multipart headers: %w", err)
		}
		kind, parameters, err := mime.ParseMediaType(parsed.Get("Content-Disposition"))
		if err != nil || kind != "form-data" {
			return nil, fmt.Errorf("invalid multipart disposition")
		}
		key := identity{parameters["name"], parameters["filename"]}
		part, exists := declared[key]
		if !exists || seen[key] || string(headers) != part.Headers || !bytes.Equal(data, part.Data) {
			return nil, fmt.Errorf("unexpected, duplicate or changed multipart part: %q %q", key.name, key.filename)
		}
		seen[key] = true
		parts = append(parts, captured{key, segment})
	}
	slices.SortFunc(parts, func(left, right captured) int {
		if order := cmp.Compare(left.key.name, right.key.name); order != 0 {
			return order
		}
		return cmp.Compare(left.key.filename, right.key.filename)
	})
	var result bytes.Buffer
	for _, part := range parts {
		result.WriteString("--<multipart-boundary>")
		result.Write(part.raw)
	}
	result.WriteString("--<multipart-boundary>--\r\n")
	return result.Bytes(), nil
}
