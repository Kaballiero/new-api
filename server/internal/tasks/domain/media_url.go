package domain

import (
	"net/url"
	"strings"
)

func IsMediaFallbackLoop(rawURL, taskID string) bool {
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsedURL == nil {
		return false
	}
	path, err := url.PathUnescape(parsedURL.EscapedPath())
	if err != nil {
		path = parsedURL.Path
	}
	if path == "/v1/videos/"+taskID+"/content" {
		return true
	}
	artifactPrefix := "/v1/tasks/" + taskID + "/artifacts/"
	return strings.HasPrefix(path, artifactPrefix) && strings.HasSuffix(path, "/content")
}
