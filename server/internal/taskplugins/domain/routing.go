package domain

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type Identity struct {
	Key          string
	ChannelTypes []int
}

func (i Identity) Platforms(pluginType int, suno string) []string {
	out := []string{i.Key}
	if i.Key == "sunoapi" {
		out = append(out, suno)
	}
	for _, kind := range i.ChannelTypes {
		if kind <= 0 || kind == pluginType {
			continue
		}
		p := strconv.Itoa(kind)
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

type OriginError struct {
	Code, Message string
	StatusCode    int
}

func (e *OriginError) Error() string { return e.Message }

type QueryError struct {
	Stage string
	Found int
	Cause error
}

func (e *QueryError) Error() string { return fmt.Sprintf("task plugin query %s", e.Stage) }
func (e *QueryError) Unwrap() error { return e.Cause }
func ResolvedIDs(value any) ([]string, bool) {
	values, ok := value.([]any)
	if !ok || len(values) > 100 {
		return nil, false
	}
	taskIDs := make([]string, len(values))
	for index, value := range values {
		taskID, stringOK := value.(string)
		if !stringOK || strings.TrimSpace(taskID) == "" {
			return nil, false
		}
		taskIDs[index] = taskID
	}
	return taskIDs, true
}

type RetrieveError struct {
	Reason string
	Cause  error
}

func (e *RetrieveError) Error() string { return "task plugin response " + e.Reason }
func (e *RetrieveError) Unwrap() error { return e.Cause }
