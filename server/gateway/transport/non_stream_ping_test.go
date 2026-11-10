package transport

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNonStreamPingWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const errorBody = `{"error":{"message":"upstream failed","type":"upstream_error"}}`
	for _, tc := range []struct {
		name           string
		interval       time.Duration
		handlerStatus  int
		awaitPing      bool
		wantStatus     int
		wantType       string
		wantLength     bool
		wantBodyPrefix bool
	}{
		{name: "fast response keeps status, headers and Content-Length", interval: time.Hour, handlerStatus: http.StatusCreated, wantStatus: http.StatusCreated, wantType: "application/json; charset=utf-8", wantLength: true},
		{name: "slow response follows whitespace pings as 200 JSON", interval: time.Millisecond, handlerStatus: http.StatusCreated, awaitPing: true, wantStatus: http.StatusOK, wantType: "application/json", wantBodyPrefix: true},
		{name: "slow error is a 200 JSON error body", interval: time.Millisecond, handlerStatus: http.StatusBadGateway, awaitPing: true, wantStatus: http.StatusOK, wantType: "application/json", wantBodyPrefix: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			middlewareStatus := make(chan int, 1)
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				c.Header(common.RequestIdKey, "request-1")
				c.Next()
				middlewareStatus <- c.Writer.Status()
			})
			engine.POST("/", func(c *gin.Context) {
				stop := startNonStreamPing(c, tc.interval)
				if tc.awaitPing {
					<-release
				}
				stop()
				c.Header("X-Upstream-Header", "upstream")
				c.Data(tc.handlerStatus, "application/json; charset=utf-8", []byte(errorBody))
			})
			server := httptest.NewServer(engine)
			t.Cleanup(server.Close)

			response, err := http.Post(server.URL, "application/json", nil)
			require.NoError(t, err)
			defer response.Body.Close()
			reader := bufio.NewReader(response.Body)
			if tc.awaitPing {
				first, err := reader.ReadByte()
				require.NoError(t, err)
				assert.Equal(t, byte(' '), first)
				close(release)
			}
			rest, err := io.ReadAll(reader)
			require.NoError(t, err)

			assert.Equal(t, tc.wantStatus, response.StatusCode)
			assert.Equal(t, tc.wantType, response.Header.Get("Content-Type"))
			assert.Equal(t, "request-1", response.Header.Get(common.RequestIdKey))
			assert.Equal(t, tc.wantLength, response.ContentLength >= 0, "Content-Length")
			assert.Equal(t, !tc.wantBodyPrefix, response.Header.Get("X-Upstream-Header") == "upstream", "headers set after pings are not sent")
			assert.Equal(t, errorBody, strings.TrimLeft(string(rest), " "))
			assert.Equal(t, tc.handlerStatus, <-middlewareStatus, "middleware sees the handler status")
		})
	}
}

func TestNonStreamPingStopsWhenClientDisconnects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlerDone := make(chan struct{})
	engine := gin.New()
	engine.POST("/", func(c *gin.Context) {
		defer close(handlerDone)
		stop := startNonStreamPing(c, time.Millisecond)
		<-c.Request.Context().Done()
		stop()
	})
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	first := make([]byte, 1)
	_, err = io.ReadFull(response.Body, first)
	require.NoError(t, err)
	cancel()
	_ = response.Body.Close()

	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("ping goroutine did not stop after the client disconnected")
	}
}
