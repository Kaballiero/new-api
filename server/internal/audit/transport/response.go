package transport

import (
	"bytes"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/audit/domain"

	"github.com/gin-gonic/gin"
)

// ResponseWriter observes only a bounded response copy. Bodies are never persisted.
type ResponseWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func capture(c *gin.Context) *ResponseWriter {
	writer := &ResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	return writer
}

func (w *ResponseWriter) Write(b []byte) (int, error) {
	if remain := 64*1024 - w.body.Len(); remain > 0 {
		w.body.Write(b[:min(len(b), remain)])
	}
	return w.ResponseWriter.Write(b)
}

func (w *ResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *ResponseWriter) result() domain.ResponseResult {
	result := domain.ResponseResult{Status: w.Status(), BufferFull: w.body.Len() == 64*1024}
	body := bytes.TrimSpace(w.body.Bytes())
	if len(body) > 0 && body[0] == '{' {
		var response struct {
			Success *bool `json:"success"`
		}
		if err := common.Unmarshal(body, &response); err == nil {
			result.Success = response.Success
		}
	}
	return result
}
