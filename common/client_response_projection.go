package common

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const clientResponseModelKey = "client_response_model"

type clientResponseModel struct {
	value string
}

func BindClientResponseModel(c *gin.Context, model string) {
	if c != nil {
		c.Set(clientResponseModelKey, clientResponseModel{value: model})
	}
}

func ProjectClientResponse(c *gin.Context, body []byte) ([]byte, error) {
	if bytes.Equal(body, []byte("[DONE]")) {
		return body, nil
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("invalid JSON client response")
	}
	model, bound := clientResponseModelFromContext(c)
	return projectClientEnvelope(body, "", model, bound)
}

func projectClientEnvelope(body []byte, path, model string, bound bool) ([]byte, error) {
	value := gjson.ParseBytes(body)
	if !value.IsObject() && !value.IsArray() {
		return body, nil
	}
	var output bytes.Buffer
	changed := false
	var projectionErr error
	first := true
	seen := map[string]bool{}
	if value.IsArray() {
		output.WriteByte('[')
	} else {
		output.WriteByte('{')
	}
	value.ForEach(func(key, child gjson.Result) bool {
		raw := []byte(child.Raw)
		if value.IsArray() {
			var err error
			raw, err = projectClientEnvelope(raw, path, model, bound)
			if err != nil {
				projectionErr = err
				return false
			}
		} else {
			name := key.String()
			childPath := projectionChildPath(path, name)
			protected := childPath != "opaque" || isProjectionField(path, name)
			if protected && seen[name] {
				projectionErr = fmt.Errorf("duplicate client response projection field %q", name)
				return false
			}
			seen[name] = true
			if isProjectionField(path, name) {
				if name != "model" && name != "modelVersion" {
					changed = true
					return true
				}
				if bound {
					changed = true
					if model == "" {
						return true
					}
					var err error
					raw, err = Marshal(model)
					if err != nil {
						projectionErr = err
						return false
					}
				}
			}
			if childPath != "opaque" {
				var err error
				raw, err = projectClientEnvelope(raw, childPath, model, bound)
				if err != nil {
					projectionErr = err
					return false
				}
			}
		}
		changed = changed || !bytes.Equal(raw, []byte(child.Raw))
		if !first {
			output.WriteByte(',')
		}
		first = false
		if value.IsObject() {
			output.WriteString(key.Raw)
			output.WriteByte(':')
		}
		output.Write(raw)
		return true
	})
	if projectionErr != nil {
		return nil, projectionErr
	}
	if !changed {
		return body, nil
	}
	if value.IsArray() {
		output.WriteByte(']')
	} else {
		output.WriteByte('}')
	}
	return output.Bytes(), nil
}

func projectionChildPath(path, key string) string {
	if path == "" && (key == "response" || key == "message" || key == "session" || key == "data" || key == "result") {
		return key
	}
	if (path == "data" || path == "data.data") && key == "result" {
		return "result"
	}
	if path == "data" && key == "data" {
		return "data.data"
	}
	if isProjectionEnvelope(path) && (key == "usage" || key == "usageMetadata") {
		return "usage"
	}
	return "opaque"
}

func isProjectionEnvelope(path string) bool {
	return path == "" || path == "response" || path == "message" || path == "session" || path == "data" || path == "data.data" || path == "result"
}

func isProjectionField(path, key string) bool {
	if key == "model" || key == "modelVersion" {
		return isProjectionEnvelope(path)
	}
	if !isProjectionEnvelope(path) && path != "usage" {
		return false
	}
	return key == "cost" || key == "cost_details" || key == "is_byok" || key == "usage_semantic" || key == "usage_source" || key == "billing_usage"
}

func clientResponseModelFromContext(c *gin.Context) (string, bool) {
	if c == nil {
		return "", false
	}
	model, ok := c.Get(clientResponseModelKey)
	if !ok {
		return "", false
	}
	projection, ok := model.(clientResponseModel)
	return projection.value, ok
}

func HasClientResponseProjection(c *gin.Context) bool {
	_, ok := clientResponseModelFromContext(c)
	return ok
}

func ProjectClientResponseString(c *gin.Context, body string) (string, error) {
	projected, err := ProjectClientResponse(c, []byte(body))
	if err != nil {
		return "", err
	}
	if bytes.Equal(projected, []byte(body)) {
		return body, nil
	}
	return string(projected), nil
}

func WriteClientJSON(c *gin.Context, status int, value any) error {
	body, err := Marshal(value)
	if err != nil {
		return err
	}
	projected, err := ProjectClientResponse(c, body)
	if err != nil {
		return err
	}
	c.Data(status, "application/json; charset=utf-8", projected)
	return nil
}
