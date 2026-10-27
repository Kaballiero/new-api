package jsplugin

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	vertexcore "github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

var taskOAuthManifest = flag.Bool("tasks-oauth-manifest", false, "list OAuth fixture cases")
var taskOAuthPreflight = flag.Bool("tasks-oauth-preflight", false, "assert provider OAuth without goldens")

func init() { fixture.RegisterUpdate(); fixture.RejectCandidateUpdate() }
func taskOAuthCases() []string {
	return []string{"cached", "expired", "acquire_error", "invalid_credentials"}
}
func TestTaskOAuthContract(t *testing.T) {
	updating := flag.Lookup("update").Value.String() == "true"
	if updating {
		fixture.CheckRecording(t)
	}
	if *taskOAuthManifest {
		data, err := common.Marshal(taskOAuthCases())
		require.NoError(t, err)
		fmt.Println("TASKS_OAUTH_MANIFEST " + string(data))
		return
	}
	if name := os.Getenv("TASKS_OAUTH_CASE"); name != "" {
		runTaskOAuthContract(t, name, updating)
		return
	}
	for _, name := range taskOAuthCases() {
		t.Run(name, func(t *testing.T) {
			exe, err := os.Executable()
			require.NoError(t, err)
			cmd := exec.CommandContext(t.Context(),
				exe,
				"-test.run=^TestTaskOAuthContract$",
				"-test.count=1",
				"-test.timeout=60s",
				"-update="+strconv.FormatBool(updating),
				"-tasks-oauth-preflight="+strconv.FormatBool(*taskOAuthPreflight))
			cmd.Env = append(os.Environ(), "TASKS_OAUTH_CASE="+name, "TZ=UTC")
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "TASKS_SNAPSHOT ") {
					fmt.Println(line)
				}
			}
		})
	}
}

type oauthContractTransport struct{ calls []map[string]any }

func (p *oauthContractTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "203.0.113.10" || r.Method != "POST" {
		return nil, errors.New("undeclared OAuth probe destination")
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	p.calls = append(p.calls, map[string]any{"method": r.Method, "url": r.URL.String(), "headers": r.Header.Clone(), "body": string(body)})
	return &http.Response{StatusCode: 200, Request: r, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"done":false}`))}, nil
}
func runTaskOAuthContract(t *testing.T, name string, updating bool) {
	httpclient.InitHttpClient()
	original := acquireAccessToken
	defer func() { acquireAccessToken = original; pluginAuthCache = sync.Map{} }()
	synctest.Test(t, func(t *testing.T) {
		pluginAuthCache = sync.Map{}
		acquired := 0
		acquireAccessToken = func(credentials vertexcore.Credentials, proxy string) (string, error) {
			acquired++
			assert.Equal(t, "test-project", credentials.ProjectID)
			assert.Empty(t, proxy)
			if name == "acquire_error" {
				return "", errors.New("fixture OAuth acquisition failed")
			}
			return fmt.Sprintf("fixture-token-%d", acquired), nil
		}
		raw, err := common.Marshal(vertexcore.Credentials{ProjectID: "test-project", ClientEmail: "fixture@example.test", PrivateKey: "test-only-secret"})
		require.NoError(t, err)
		if name == "invalid_credentials" {
			raw = []byte(`{`)
		}
		source, err := plugins.Source("vertex-ai")
		require.NoError(t, err)
		plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
		require.NoError(t, err)
		adaptor := New(plugin)
		adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://203.0.113.10", ApiKey: string(raw)}})
		operation := "projects/test-project/locations/us-central1/publishers/google/models/veo-3.0-generate-001/operations/upstream-fixture"
		task := &model.Task{TaskID: "task_vertex",
			Properties: model.Properties{OriginModelName: "veo-3.0-generate-001",
				UpstreamModelName: "veo-3.0-generate-001"},
			PrivateData: model.TaskPrivateData{UpstreamTaskID: base64.RawURLEncoding.EncodeToString([]byte(operation))}}
		transport := &oauthContractTransport{calls: []map[string]any{}}
		httpclient.GetHttpClient().Transport = transport
		results := []any{}
		for step := range 2 {
			if name == "expired" && step == 1 {
				time.Sleep(26 * time.Minute)
			}
			response, err := adaptor.FetchTask("https://203.0.113.10", string(raw), task, "")
			if name == "acquire_error" || name == "invalid_credentials" {
				require.Error(t, err)
				require.Nil(t, response)
			} else {
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
			}
			var failure any
			if err != nil {
				failure = err.Error()
			}
			results = append(results, map[string]any{"error": failure, "acquired": acquired, "outbound": append([]map[string]any{}, transport.calls...)})
		}
		switch name {
		case "cached":
			assert.Equal(t, 1, acquired)
		case "expired", "acquire_error":
			assert.Equal(t, 2, acquired)
		case "invalid_credentials":
			assert.Zero(t, acquired)
		}
		snapshot, err := common.Marshal(results)
		require.NoError(t, err)
		assert.NotContains(t, string(snapshot), "test-only-secret")
		fixture.Compare(t, "testdata/tasks-oauth", name, results, *taskOAuthPreflight, updating)
	})
}
