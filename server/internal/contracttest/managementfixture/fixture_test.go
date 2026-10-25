package managementfixture

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovedNotificationLogBaselines(t *testing.T) {
	if mode := os.Getenv("WAVE6C_LOG_PROBE"); mode != "" {
		actual := `{"logs":"current","quota":0,"empty":[],"absent":null,"raw":"{\"zero\":0}"}`
		if mode == "origin" {
			actual = `{"logs":"original","quota":0,"empty":[],"absent":null,"raw":"{\"zero\":0}"}`
		}
		if mode == "changed-log" {
			actual = `{"logs":"changed","quota":0,"empty":[],"absent":null,"raw":"{\"zero\":0}"}`
		}
		if mode == "changed-state" {
			actual = `{"logs":"current","quota":1,"empty":[],"absent":null,"raw":"{\"zero\":0}"}`
		}
		compareNotificationLogs(t, "data", "root_channel_disabled", []byte(actual))
		return
	}
	for _, mode := range []string{"candidate", "origin", "changed-log", "changed-state", "missing-case"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "service", "data")
			require.NoError(t, os.MkdirAll(directory, 0700))
			golden := `{"logs":"original","quota":0,"empty":[],"absent":null,"raw":"{\"zero\":0}"}`
			pretty, err := common.IndentJson([]byte(golden))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(directory, "root_channel_disabled.golden.json"), pretty, 0600))
			original := `{"root_channel_disabled":"original","root_channel_autoban_off":"original other"}`
			current := `{"root_channel_disabled":"current","root_channel_autoban_off":"current other"}`
			if mode == "missing-case" {
				current = `{"root_channel_disabled":"current"}`
			}
			require.NoError(t, os.WriteFile(filepath.Join(directory, "origin-logs-baseline.json"), []byte(original), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "pre-move-logs-baseline.json"), []byte(current), 0600))
			layout := "candidate"
			if mode == "origin" {
				layout = "origin"
				require.NoError(t, os.WriteFile(filepath.Join(root, ".tasks-origin"), []byte(taskfixture.Origin+"\n"), 0600))
			}
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestApprovedNotificationLogBaselines$", "-test.count=1")
			command.Dir = filepath.Join(root, "service")
			command.Env = append(os.Environ(), "WAVE6C_LOG_PROBE="+mode, "WAVE6C_LAYOUT="+layout)
			output, err := command.CombinedOutput()
			if mode == "origin" || mode == "candidate" {
				require.NoError(t, err, "%s", output)
			} else {
				require.Error(t, err, "invalid baseline/state must fail: %s", output)
			}
		})
	}
}

func TestWireUnownedRequestsDoNotPoisonEmptyFixture(t *testing.T) {
	transport := &Transport{wireToken: "fixture-A", Pending: map[int]wireReply{}}
	for _, identity := range []string{"", "fixture-B"} {
		request := httptest.NewRequest(http.MethodGet, "http://fixture.invalid/", nil)
		request.Header.Set("X-Wave6c-Fixture", identity)
		request.Header.Set("X-Wave6c-Reply", "1")
		response := httptest.NewRecorder()
		transport.serveWire(response, request)
		assert.Equal(t, http.StatusNotFound, response.Code)
	}
	assert.Empty(t, transport.Requests)
	assert.Empty(t, transport.Pending)
	assert.Empty(t, transport.WireErrors)
	assert.Zero(t, transport.Served)
	assert.Equal(t, 2, transport.rejected)
}

func TestWireOwnedRequestsRemainStrict(t *testing.T) {
	for _, change := range []string{"index", "body", "host", "header", "replay"} {
		t.Run(change, func(t *testing.T) {
			transport := &Transport{wireToken: "fixture-A", Pending: map[int]wireReply{1: {Status: 200, Body: "ok"}},
				Requests: []Request{{Method: "POST", URL: "http://fixture.invalid/path?q=1", Body: "exact",
					Header: http.Header{"X-Expected": {"value"}}}}}
			request := httptest.NewRequest("POST", "http://fixture.invalid/path?q=1", strings.NewReader("exact"))
			request.Header.Set("X-Wave6c-Fixture", "fixture-A")
			request.Header.Set("X-Wave6c-Reply", "1")
			request.Header.Set("X-Expected", "value")
			switch change {
			case "index":
				request.Header.Set("X-Wave6c-Reply", "invalid")
			case "body":
				request.Body = io.NopCloser(strings.NewReader("changed"))
			case "host":
				request.Host = "other.invalid"
			case "header":
				request.Header.Set("X-Expected", "changed")
			case "replay":
				delete(transport.Pending, 1)
			}
			transport.serveWire(httptest.NewRecorder(), request)
			require.NotEmpty(t, transport.WireErrors, "owned corruption must never become ignored traffic")
			assert.Zero(t, transport.rejected)
		})
	}
}

func TestWireParallelFixturesKeepOriginalRequests(t *testing.T) {
	t.Setenv("WAVE6C_REAL_HTTP", "1")
	t.Setenv("WAVE6C_WIRE_NETWORK", "pipe")
	t.Setenv("WAVE6C_WIRE_PROBES", "1")
	transports := []*Transport{}
	for i := range 2 {
		body := fmt.Sprint(i)
		transport := &Transport{Reply: func(*http.Request, int) (int, string, error) { return 200, body, nil }}
		transport.EnableServer(t)
		transports = append(transports, transport)
	}
	require.NotEqual(t, transports[0].wireToken, transports[1].wireToken)
	errors := make(chan error, len(transports))
	var workers sync.WaitGroup
	for i, transport := range transports {
		workers.Go(func() {
			request, err := http.NewRequest("POST", "https://fixture.invalid/path?q=1", strings.NewReader(fmt.Sprint(i)))
			if err == nil {
				request.Header.Set("X-Expected", "value")
				var response *http.Response
				response, err = transport.RoundTrip(request)
				if err == nil {
					var body []byte
					body, err = io.ReadAll(response.Body)
					response.Body.Close()
					if string(body) != fmt.Sprint(i) {
						err = fmt.Errorf("response reached the wrong fixture: %q", body)
					}
				}
			}
			errors <- err
		})
	}
	workers.Wait()
	for range transports {
		require.NoError(t, <-errors)
	}
	for i, transport := range transports {
		require.Len(t, transport.Requests, 1)
		assert.Equal(t, fmt.Sprint(i), transport.Requests[0].Body)
		assert.Empty(t, transport.Requests[0].Header.Get("X-Wave6c-Fixture"))
		assert.Empty(t, transport.Requests[0].Header.Get("X-Wave6c-Reply"))
		assert.Equal(t, "https://fixture.invalid/path?q=1", transport.Requests[0].URL)
		assert.Equal(t, 2, transport.rejected)
		assert.Equal(t, 1, transport.Served)
		assert.Empty(t, transport.Pending)
	}
}

func TestWireLostIdentityCannotPass(t *testing.T) {
	if os.Getenv("WAVE6C_LOST_WIRE_IDENTITY") == "1" {
		t.Setenv("WAVE6C_REAL_HTTP", "1")
		t.Setenv("WAVE6C_WIRE_NETWORK", "pipe")
		t.Setenv("WAVE6C_WIRE_PROBES", "")
		transport := &Transport{Reply: func(*http.Request, int) (int, string, error) { return 200, "ok", nil }}
		transport.EnableServer(t)
		transport.Client.Transport = lostWireIdentity{transport.Client.Transport}
		request, err := http.NewRequest("GET", "http://fixture.invalid/path", nil)
		require.NoError(t, err)
		response, err := transport.RoundTrip(request)
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, response.StatusCode)
		require.NoError(t, response.Body.Close())
		return // Cleanup must reject the undelivered registered request.
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWireLostIdentityCannotPass$", "-test.count=1")
	command.Env = append(os.Environ(), "WAVE6C_LOST_WIRE_IDENTITY=1")
	output, err := command.CombinedOutput()
	require.Error(t, err, "lost identity must never produce a successful test: %s", output)
	require.Contains(t, string(output), "every registered request must reach its own listener")
}

type lostWireIdentity struct{ http.RoundTripper }

func (transport lostWireIdentity) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Del("X-Wave6c-Fixture")
	return transport.RoundTripper.RoundTrip(request)
}

func TestMetadataFixtureAllowsVirtualRetryAfterHTTPError(t *testing.T) {
	listener := &wirePipeListener{clients: make(chan net.Conn), closed: make(chan struct{})}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(
		func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusInternalServerError) })}}
	startMetadataFixtureServer(t, server)
	client := &http.Client{Transport: &http.Transport{DialContext: listener.dialContext}}
	t.Cleanup(client.CloseIdleConnections)
	synctest.Test(t, func(t *testing.T) {
		response, err := client.Get(server.URL + "/api/i18n/en/newapi/models.json")
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, response.StatusCode)
		require.True(t, response.Close, "fixture must close its connection, not leave an idle reader in the bubble")
		require.NoError(t, response.Body.Close())
		start := time.Now()
		time.Sleep(200 * time.Millisecond)
		require.Equal(t, 200*time.Millisecond, time.Since(start))
	})
}

func TestMetadataFixtureRejectsProbesBeforeCapture(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	var mu sync.Mutex
	captured := []Request{}
	listener := &wirePipeListener{clients: make(chan net.Conn), closed: make(chan struct{})}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		captured = append(captured, Request{Method: request.Method, URL: request.URL.RequestURI(), Header: request.Header.Clone()})
		mu.Unlock()
		if strings.HasSuffix(request.URL.Path, ".json") {
			entered <- struct{}{}
			<-release
		}
		io.WriteString(writer, "metadata")
	})}}
	startMetadataFixtureServer(t, server)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	client := &http.Client{Transport: &http.Transport{DialContext: listener.dialContext}}
	t.Cleanup(client.CloseIdleConnections)
	results := make(chan error, 2)
	for _, endpoint := range []string{"models.json", "vendors.json"} {
		go func() {
			response, err := client.Get(server.URL + "/api/i18n/en/newapi/" + endpoint)
			if err == nil {
				if response.StatusCode != http.StatusOK {
					err = fmt.Errorf("metadata request returned %d", response.StatusCode)
				}
				response.Body.Close()
			}
			results <- err
		}()
	}
	for range 2 {
		<-entered
	}
	// Both metadata captures are in progress while the unowned probes arrive.
	for _, path := range []string{"/", "/", "/unknown", "/api/i18n/en/newapi/models.json?probe=1"} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+path, nil)
		require.NoError(t, err)
		request.Close = true
		response, err := client.Do(request)
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, response.StatusCode)
		assert.True(t, response.Close)
		require.NoError(t, response.Body.Close())
	}
	releaseOnce.Do(func() { close(release) })
	for range 2 {
		require.NoError(t, <-results)
	}
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, captured, 2)
	assert.ElementsMatch(t, []string{"/api/i18n/en/newapi/models.json", "/api/i18n/en/newapi/vendors.json"}, []string{captured[0].URL, captured[1].URL})
	for _, request := range captured {
		assert.Empty(t, request.Header.Get("Connection"))
	}
}
