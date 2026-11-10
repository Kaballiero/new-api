package transport

import (
	"context"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// nonStreamPingRelayFormats answer a non-stream request with a JSON body, so
// leading whitespace keeps the body valid. Audio (binary speech, text/srt/vtt
// transcriptions) and Realtime (WebSocket) are excluded.
var nonStreamPingRelayFormats = []types.RelayFormat{
	types.RelayFormatOpenAI,
	types.RelayFormatClaude,
	types.RelayFormatGemini,
	types.RelayFormatOpenAIResponses,
	types.RelayFormatOpenAIResponsesCompaction,
	types.RelayFormatOpenAIAlphaSearch,
	types.RelayFormatOpenAIImage,
	types.RelayFormatSystemOne,
	types.RelayFormatEmbedding,
	types.RelayFormatRerank,
}

var nonStreamPingData = []byte(" ")

// nonStreamPingWriter keeps a non-stream JSON response alive behind proxies
// that drop idle connections. If the handler has not started its response
// within one interval, the writer commits 200 application/json and sends one
// JSON whitespace byte per interval; the handler body follows the pings, and
// its own status and headers are no longer sent. Status reports the handler
// status for middleware and logs.
//
// The handler sees a private header map, so the ping goroutine never shares a
// map with it; all writes to the client go under mu.
type nonStreamPingWriter struct {
	gin.ResponseWriter

	handlerHeader http.Header
	stopPing      chan struct{}
	pinger        sync.WaitGroup

	mu        sync.Mutex
	status    int
	pinging   bool
	responded bool
}

// startNonStreamPing replaces c.Writer with a nonStreamPingWriter and starts
// pinging; stop ends the pings and waits for the ping goroutine.
func startNonStreamPing(c *gin.Context, interval time.Duration) (stop func()) {
	w := &nonStreamPingWriter{
		ResponseWriter: c.Writer,
		handlerHeader:  c.Writer.Header().Clone(),
		stopPing:       make(chan struct{}),
		status:         c.Writer.Status(),
	}
	c.Writer = w
	ctx := c.Request.Context()
	w.pinger.Go(func() { w.ping(ctx, interval) })
	return func() {
		close(w.stopPing)
		w.pinger.Wait()
	}
}

func (w *nonStreamPingWriter) ping(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopPing:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if !w.writePing() {
			return
		}
	}
}

// writePing sends one ping and reports whether pinging should continue.
func (w *nonStreamPingWriter) writePing() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.responded {
		return false
	}
	if !w.pinging {
		header := w.ResponseWriter.Header()
		header.Del("Content-Length")
		header.Set("Content-Type", "application/json")
		w.ResponseWriter.WriteHeader(http.StatusOK)
		w.pinging = true
	}
	if _, err := w.ResponseWriter.Write(nonStreamPingData); err != nil {
		return false
	}
	w.ResponseWriter.Flush()
	return true
}

// respond stops pinging before the first handler write. Without pings the
// handler status and headers are sent as usual. The caller holds mu.
func (w *nonStreamPingWriter) respond() {
	if w.responded {
		return
	}
	w.responded = true
	if w.pinging {
		return
	}
	header := w.ResponseWriter.Header()
	clear(header)
	maps.Copy(header, w.handlerHeader)
	w.ResponseWriter.WriteHeader(w.status)
}

func (w *nonStreamPingWriter) Header() http.Header {
	return w.handlerHeader
}

func (w *nonStreamPingWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.responded || code <= 0 {
		return
	}
	w.status = code
	if !w.pinging {
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *nonStreamPingWriter) WriteHeaderNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.respond()
	w.ResponseWriter.WriteHeaderNow()
}

func (w *nonStreamPingWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.respond()
	return w.ResponseWriter.Write(data)
}

func (w *nonStreamPingWriter) WriteString(s string) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.respond()
	return w.ResponseWriter.WriteString(s)
}

func (w *nonStreamPingWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.respond()
	w.ResponseWriter.Flush()
}

func (w *nonStreamPingWriter) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

func (w *nonStreamPingWriter) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.responded
}

func (w *nonStreamPingWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
