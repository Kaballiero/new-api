// Package managementfixture provides portable management-contract process and oracle boundaries.
package managementfixture

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	taskfixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

var manifest = flag.Bool("management-manifest", false, "list cases before fixtures")
var preflight = flag.Bool("management-preflight", false, "capture without oracle IO")
var originCheck = flag.Bool("management-origin-check", false, "validate origin before fixtures")

func init() { taskfixture.RejectCandidateUpdate() }

// Run isolates mutable settings, database globals, caches and limiters in child processes.
func Run(t *testing.T, root, group, directory string, names []string, locales []string, execute func(*testing.T, string) any) {
	t.Helper()
	updating := flag.Lookup("update").Value.String() == "true"
	if updating || *originCheck {
		taskfixture.CheckRecording(t)
	}
	if *originCheck {
		return
	}
	require.NotEmpty(t, names)
	require.Equal(t, len(names), len(slices.Compact(slices.Sorted(slices.Values(names)))))
	if *manifest {
		data, err := common.Marshal(names)
		require.NoError(t, err)
		fmt.Println("WAVE6C_MANIFEST " + group + " " + string(data))
		return
	}
	if name := os.Getenv("WAVE6C_CASE"); name != "" {
		require.Contains(t, names, name)
		Compare(t, directory, name, execute(t, name))
		return
	}
	if path := os.Getenv("WAVE6C_LOCALES"); path != "" {
		values := map[string]json.RawMessage{}
		for _, language := range []string{"default", "en", "ru", "zh-CN", "zh-TW", "unknown"} {
			for _, name := range locales {
				require.Contains(t, names, name)
				values[language+"/"+name] = child(t, root, name, false, true, language)
			}
		}
		data, err := common.Marshal(values)
		require.NoError(t, err)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		require.NoError(t, err)
		_, err = file.Write(data)
		require.NoError(t, err)
		require.NoError(t, file.Close())
		return
	}
	selected := names
	if filter := os.Getenv("WAVE6C_SELECTION"); filter != "" {
		require.False(t, updating, "partial recording is forbidden")
		selected = strings.Split(filter, ",")
		for _, name := range selected {
			require.Contains(t, names, name)
		}
	}
	for _, name := range selected {
		t.Run(name, func(t *testing.T) { t.Parallel(); child(t, root, name, updating, *preflight, "") })
	}
}

func child(t *testing.T, root, name string, updating, capture bool, language string) json.RawMessage {
	t.Helper()
	scratch := t.TempDir()
	childContext := t.Context()
	timeout := "90s"
	metadata := root == "TestCatalogContract" && strings.HasPrefix(name, "sync_")
	if metadata {
		var cancel context.CancelFunc
		childContext, cancel = context.WithTimeout(childContext, 20*time.Second)
		defer cancel()
		timeout = "15s"
	}
	command := exec.CommandContext(childContext, os.Args[0], "-test.run=^"+root+"$", "-test.count=1", "-test.timeout="+timeout,
		"-update="+strconv.FormatBool(updating), "-management-preflight="+strconv.FormatBool(capture))
	command.Env = append(os.Environ(), "WAVE6C_CASE="+name, "WAVE6C_LANG="+language, "WAVE6C_LOCALES=", "WAVE6C_SELECTION=", "TMPDIR="+scratch)
	output, err := command.CombinedOutput()
	if metadata && (childContext.Err() == context.DeadlineExceeded || strings.Contains(string(output), "panic: test timed out")) {
		require.FailNow(t, "WAVE6C_METADATA_TIMEOUT: "+name,
			"metadata child exceeded 15s (20s parent watchdog); check fixture keep-alive/virtual-clock networking; output:\n%s", output)
	}
	require.NoError(t, err, "%s", output)
	var values []json.RawMessage
	completed := 0
	for line := range strings.SplitSeq(string(output), "\n") {
		if raw, ok := strings.CutPrefix(line, "WAVE6C_SNAPSHOT "+name+" "); ok {
			values = append(values, json.RawMessage(raw))
			fmt.Println(line)
		}
		if line == "WAVE6C_CHILD_COMPLETE "+name {
			completed++
		}
		if strings.HasPrefix(line, "WAVE6C_ORDER ") || strings.HasPrefix(line, "WAVE6C_WIRE ") || strings.HasPrefix(line, "WAVE6C_METADATA ") {
			fmt.Println(line)
		}
	}
	require.Equal(t, 1, completed, "child must finish every assertion and comparison")
	require.Len(t, values, 1)
	return values[0]
}

// Compare always emits the exact compared snapshot, including in no-update runs.
func Compare(t *testing.T, directory, name string, value any) {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	updating := flag.Lookup("update").Value.String() == "true"
	if !*preflight && !updating && directory == "testdata/notifications" &&
		(name == "root_channel_disabled" || name == "root_channel_autoban_off") {
		compareNotificationLogs(t, directory, name, data)
	} else {
		taskfixture.Compare(t, directory, name, json.RawMessage(data), *preflight, updating)
	}
	fmt.Println("WAVE6C_SNAPSHOT " + name + " " + string(data))
	fmt.Println("WAVE6C_CHILD_COMPLETE " + name)
}

// compareNotificationLogs checks the two approved raw log baselines independently.
// The remaining origin snapshot fields are compared without translating or rewriting their text.
func compareNotificationLogs(t *testing.T, directory, name string, data []byte) {
	t.Helper()
	layout := os.Getenv("WAVE6C_LAYOUT")
	if layout == "" {
		layout = "candidate"
	}
	require.Contains(t, []string{"origin", "candidate"}, layout)
	marker, markerErr := os.ReadFile("../.tasks-origin")
	if layout == "origin" {
		require.NoError(t, markerErr)
		require.Equal(t, taskfixture.Origin+"\n", string(marker))
	} else {
		require.True(t, os.IsNotExist(markerErr), "candidate cannot use an origin marker")
	}
	baselines := map[string]map[string]string{}
	for _, source := range []string{"origin", "pre-move"} {
		raw, err := os.ReadFile(filepath.Join(directory, source+"-logs-baseline.json"))
		require.NoError(t, err, "host must capture both approved log baselines first")
		var values map[string]string
		require.NoError(t, common.Unmarshal(raw, &values))
		require.Len(t, values, 2)
		require.Contains(t, values, "root_channel_disabled")
		require.Contains(t, values, "root_channel_autoban_off")
		baselines[source] = values
	}
	expectedData, err := os.ReadFile(filepath.Join(directory, name+".golden.json"))
	require.NoError(t, err)
	var expected, actual map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(expectedData, &expected))
	require.NoError(t, common.Unmarshal(data, &actual))
	var originLog, actualLog string
	require.NoError(t, common.Unmarshal(expected["logs"], &originLog))
	require.NoError(t, common.Unmarshal(actual["logs"], &actualLog))
	require.Equal(t, baselines["origin"][name], originLog)
	source := "pre-move"
	if layout == "origin" {
		source = "origin"
	}
	require.Equal(t, baselines[source][name], actualLog)
	delete(expected, "logs")
	delete(actual, "logs")
	for _, value := range []map[string]json.RawMessage{expected, actual} {
		for field, raw := range value {
			value[field], err = common.Marshal(raw)
			require.NoError(t, err)
		}
	}
	require.Equal(t, expected, actual, "non-log snapshot fields must match origin")
}

// State preserves rows and raw JSON columns while excluding credential material unrelated to these operations.
func State(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"users":     taskfixture.Rows(t, model.DB, "users", "id,username,role,status,quota,used_quota,request_count,group,email,setting", "id"),
		"models":    taskfixture.Rows(t, model.DB, "models", "*", "id"),
		"vendors":   taskfixture.Rows(t, model.DB, "vendors", "*", "id"),
		"prefill":   taskfixture.Rows(t, model.DB, "prefill_groups", "*", "id"),
		"channels":  taskfixture.Rows(t, model.DB, "channels", "id,type,status,models,group,priority,weight,setting,model_mapping,used_quota", "id"),
		"abilities": taskfixture.Rows(t, model.DB, "abilities", "*", `model,channel_id,"group"`),
		"options":   taskfixture.Rows(t, model.DB, "options", "*", "key"),
		"audit":     taskfixture.Rows(t, model.LOG_DB, "audit_logs", "*", "id"),
		"logs":      taskfixture.Rows(t, model.LOG_DB, "logs", "id,user_id,quota,type,content", "id"),
	}
}

// Request records original URLs and exact payloads, independent of the local fixture listener address.
type Request struct {
	Method string      `json:"method"`
	URL    string      `json:"url"`
	Header http.Header `json:"header"`
	Body   string      `json:"body"`
}

// Transport captures a bounded fake provider without production hooks or live provider access.
type Transport struct {
	Server     *httptest.Server
	Client     *http.Client
	Pending    map[int]wireReply
	WireErrors []string
	Served     int
	Mu         sync.Mutex
	Requests   []Request
	Reply      func(*http.Request, int) (int, string, error)
	wireToken  string
	wireIssued int
	rejected   int
}

func (transport *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	var data []byte
	var err error
	if request.Body != nil {
		data, err = io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
	}
	header := request.Header.Clone()
	transport.Mu.Lock()
	transport.Requests = append(transport.Requests, Request{request.Method, request.URL.String(), header, string(data)})
	index := len(transport.Requests)
	transport.Mu.Unlock()
	status, body, err := transport.Reply(request, index)
	if err != nil {
		return nil, err
	}
	if transport.Server != nil {
		transport.Mu.Lock()
		transport.Pending[index] = wireReply{status, body}
		transport.wireIssued++
		transport.Mu.Unlock()
		copy := request.Clone(request.Context())
		destination, parseErr := url.Parse(transport.Server.URL)
		if parseErr != nil {
			return nil, parseErr
		}
		copy.URL.Scheme, copy.URL.Host = destination.Scheme, destination.Host
		copy.Host = request.URL.Host
		copy.Header = copy.Header.Clone()
		copy.Header.Set("X-Wave6c-Reply", strconv.Itoa(index))
		copy.Header.Set("X-Wave6c-Fixture", transport.wireToken)
		copy.Body = io.NopCloser(strings.NewReader(string(data)))
		copy.ContentLength = int64(len(data))
		return transport.Client.Do(copy)
	}
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d fixture", status),
		Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

// CanonicalOrder compares only a validated permutation; callers must prove stability on both layouts.
func CanonicalOrder(t *testing.T, name string, requests []Request) []Request {
	t.Helper()
	data, err := common.Marshal(requests)
	require.NoError(t, err)
	fmt.Println("WAVE6C_ORDER " + name + " " + string(data))
	result := slices.Clone(requests)
	slices.SortFunc(result, func(a, b Request) int {
		left, _ := common.Marshal(a)
		right, _ := common.Marshal(b)
		return strings.Compare(string(left), string(right))
	})
	return result
}

// FixturePaths masks only an explicitly owned temporary listener origin after containment checks.
func FixturePaths(t *testing.T, value any, origin string) json.RawMessage {
	t.Helper()
	require.True(t, strings.HasPrefix(origin, "http://127.0.0.1:") || strings.HasPrefix(origin, "https://127.0.0.1:"))
	data, err := common.Marshal(value)
	require.NoError(t, err)
	return json.RawMessage(strings.ReplaceAll(string(data), origin, "http://fixture.invalid"))
}

// Locale returns the explicitly requested locale; canonical parity is always Chinese.
func Locale() string {
	if value := os.Getenv("WAVE6C_LANG"); value != "" {
		return value
	}
	return "zh-CN"
}

type wireReply struct {
	Status int
	Body   string
}

// NewMetadataFixtureServer leaves no idle network reader inside the virtual clock bubble.
func NewMetadataFixtureServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	startMetadataFixtureServer(t, server)
	if os.Getenv("WAVE6C_METADATA_PROBES") == "1" {
		for range 2 {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/", nil)
			require.NoError(t, err)
			request.Close = true
			response, err := server.Client().Do(request)
			require.NoError(t, err)
			require.Equal(t, http.StatusNotFound, response.StatusCode)
			require.NoError(t, response.Body.Close())
		}
		fmt.Printf("WAVE6C_METADATA %s rejected_probes=2\n", os.Getenv("WAVE6C_CASE"))
	}
	return server
}

func startMetadataFixtureServer(t *testing.T, server *httptest.Server) {
	t.Helper()
	handler := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Admit fixture endpoints before calling the handler that records outbound requests.
		// Unrelated loopback probes must never enter that capture or affect fixture state.
		if request.Method != http.MethodGet || request.URL.RawQuery != "" ||
			(request.URL.EscapedPath() != "/api/i18n/en/newapi/models.json" &&
				request.URL.EscapedPath() != "/api/i18n/en/newapi/vendors.json") {
			writer.Header().Set("Connection", "close")
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		handler.ServeHTTP(writer, request)
	})
	// The real metadata fetcher sleeps even after its final failed attempt.
	// Idle network I/O prevents synctest from advancing that retry timer.
	server.Config.SetKeepAlivesEnabled(false)
	server.Start()
	t.Cleanup(server.Close)
}

// EnableServer adds real listener/wire validation on host; it is never used for a live provider.
func (transport *Transport) EnableServer(t *testing.T) {
	t.Helper()
	if os.Getenv("WAVE6C_REAL_HTTP") != "1" {
		return
	}
	clientTransport := &http.Transport{DisableKeepAlives: true}
	var server *httptest.Server
	if os.Getenv("WAVE6C_WIRE_NETWORK") == "pipe" {
		listener := &wirePipeListener{clients: make(chan net.Conn), closed: make(chan struct{})}
		clientTransport.DialContext = listener.dialContext
		server = &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(transport.serveWire)}}
	} else {
		require.Empty(t, os.Getenv("WAVE6C_WIRE_NETWORK"), "recording uses TCP; pipe mode is explicit sandbox evidence only")
		server = httptest.NewUnstartedServer(http.HandlerFunc(transport.serveWire))
	}
	transport.Pending = map[int]wireReply{}
	transport.wireToken = rand.Text()
	transport.Client = &http.Client{Transport: clientTransport}
	transport.Server = server
	server.Start()
	t.Cleanup(func() {
		transport.Server.Close()
		transport.Client.CloseIdleConnections()
		transport.Mu.Lock()
		defer transport.Mu.Unlock()
		fmt.Printf("WAVE6C_WIRE %s served=%d issued=%d rejected=%d pending=%d errors=%d\n",
			os.Getenv("WAVE6C_CASE"), transport.Served, transport.wireIssued, transport.rejected,
			len(transport.Pending), len(transport.WireErrors))
		require.Empty(t, transport.WireErrors)
		require.Empty(t, transport.Pending, "every registered request must reach its own listener")
		require.Equal(t, transport.wireIssued, transport.Served, "each registered request must be served exactly once")
	})
	if os.Getenv("WAVE6C_WIRE_PROBES") == "1" {
		for range 2 {
			response, err := transport.Client.Get(server.URL + "/")
			require.NoError(t, err)
			require.Equal(t, http.StatusNotFound, response.StatusCode)
			require.NoError(t, response.Body.Close())
		}
	}
}

// serveWire validates owned wire requests; unrelated loopback traffic has no fixture identity.
func (transport *Transport) serveWire(writer http.ResponseWriter, request *http.Request) {
	if request.Header.Get("X-Wave6c-Fixture") != transport.wireToken {
		transport.Mu.Lock()
		transport.rejected++
		transport.Mu.Unlock()
		writer.Header().Set("Connection", "close")
		writer.WriteHeader(http.StatusNotFound)
		return
	}
	raw, err := io.ReadAll(request.Body)
	index, parseErr := strconv.Atoi(request.Header.Get("X-Wave6c-Reply"))
	transport.Mu.Lock()
	defer transport.Mu.Unlock()
	reply, pending := transport.Pending[index]
	if err != nil || parseErr != nil || index < 1 || index > len(transport.Requests) || !pending {
		transport.WireErrors = append(transport.WireErrors, fmt.Sprintf(
			"invalid fixture wire identity/body: index=%q registered=%d pending=%t read_error=%t",
			request.Header.Get("X-Wave6c-Reply"), len(transport.Requests), pending, err != nil))
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	expected := transport.Requests[index-1]
	original, _ := url.Parse(expected.URL)
	if request.Method != expected.Method || request.URL.RequestURI() != original.RequestURI() ||
		string(raw) != expected.Body || request.Host != original.Host {
		transport.WireErrors = append(transport.WireErrors, "wire request changed")
	}
	for name, values := range expected.Header {
		for _, value := range values {
			if request.Header.Get(name) != strings.TrimSpace(value) {
				transport.WireErrors = append(transport.WireErrors, "wire header changed: "+name)
			}
		}
	}
	delete(transport.Pending, index)
	transport.Served++
	writer.WriteHeader(reply.Status)
	io.WriteString(writer, reply.Body)
}

// wirePipeListener exercises HTTP/1 framing without claiming TCP/socket acceptance.
// Connections are created by the server outside the virtual clock bubble.
type wirePipeListener struct {
	clients chan net.Conn
	closed  chan struct{}
	once    sync.Once
}

func (listener *wirePipeListener) Accept() (net.Conn, error) {
	server, client := net.Pipe()
	select {
	case listener.clients <- client:
		return server, nil
	case <-listener.closed:
		server.Close()
		client.Close()
		return nil, net.ErrClosed
	}
}

func (listener *wirePipeListener) Close() error {
	listener.once.Do(func() { close(listener.closed) })
	return nil
}

func (*wirePipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
}

func (listener *wirePipeListener) dialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	select {
	case connection := <-listener.clients:
		return connection, nil
	case <-listener.closed:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
