// Package pluginfixture contains portable task-plugin contract fixtures, never production wiring.
package pluginfixture

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	pluginsapp "github.com/QuantumNous/new-api/internal/app/taskplugins"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	pluginsdomain "github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"

	"github.com/stretchr/testify/require"
)

const Key = "contract-plugin"
const Model = "contract-video"
const Token = "pluginsfixturekey0000000000000000000000000000000"
const ProviderKey = "sk-test-plugin-provider-secret"

// Source exercises the actual native and host-protocol surfaces with one test-only driver.
func Source(key, version string) string {
	source := fmt.Sprintf(`
export const meta = {apiVersion:1,key:%q,name:"Contract plugin",version:%q,author:{name:"Test"},
models:["contract-video"],baseUrl:"https://plugins.invalid",fetchMode:"per_task",sortPriority:100,
routes:[{method:"POST",path:"/wave5c/submit",type:"submit",decode:"decode",render:"created"},
{method:"GET",path:"/wave5c/query/:task_id",type:"query",render:"query"},
{method:"POST",path:"/wave5c/query",type:"dynamic",decode:"batch",render:"queries"}],
protocols:[{name:"openai_responses",supports:["stream","sync","background"]},"openai_video"]};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit",method:"POST",
headers:{Authorization:"Bearer "+ctx.apiKey,"Content-Type":"application/json"},body:ctx.requestBody};}
export function parseSubmitResponse(ctx,resp){return {taskId:resp.body.id,taskData:resp.body};}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/poll/"+ctx.taskId,method:"GET",
headers:{Authorization:"Bearer "+ctx.apiKey}};}
export function parseTaskResult(ctx,body){return {status:body.status||"IN_PROGRESS",progress:"50%%"};}
export function listArtifacts(){return [];}
export function buildContentRequest(){throw new Error("artifact_not_found");}
export const native={decode:function(ctx){let b=ctx.body.value;return {kind:"submit",model:b.model,
action:"text_to_video",requestBody:b,originTaskIds:b.origins||[]};},
batch:function(ctx){return {kind:"query",taskIds:ctx.body.value.ids};},
created:function(ctx,task){return {task:task};},query:function(ctx,task){return task;},
queries:function(ctx,tasks){return tasks;},error:function(ctx,error){return {error:error};}};
export const protocols={openai_responses:{decodeRequest:function(ctx){return {kind:"submit",model:ctx.model,
requestBody:ctx.body.value,action:"text_to_video"};},renderFinal:function(){return {output:[]};},
renderEvents:function(ctx,task){return {events:[],done:task.status==="SUCCESS"};}},
openai_video:{decodeRequest:function(ctx){return {kind:"submit",model:ctx.model,requestBody:ctx.body.value};},
render:function(ctx,task){return {id:task.task_id,object:"video",status:task.status};}}};
export function echo(value){return value;}
`, key, version)
	if key != Key {
		source = strings.ReplaceAll(source, "/wave5c/", "/"+key+"/")
		source = strings.ReplaceAll(source, strconv.Quote(Model), strconv.Quote(key+"-video"))
	}
	return source
}

func Register(t *testing.T, key, version string) *jsplugin.LoadedPlugin {
	t.Helper()
	loaded, err := jsplugin.DefaultRegistry.Register(Source(key, version), jsplugin.Options{})
	require.NoError(t, err)
	return loaded
}

func Save(t *testing.T, key, version string, enabled bool) *model.TaskPlugin {
	t.Helper()
	source := Source(key, version)
	plugin := &model.TaskPlugin{Key: key, Version: version, APIVersion: 1, Source: source,
		SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(source))), Enabled: enabled, Remark: "fixture"}
	require.NoError(t, pluginsapp.NewStore().SaveTaskPlugin((*pluginsdomain.Plugin)(plugin)))
	return plugin
}

// Processes isolates caches, registry generations and production rate-limit state per case.
func Processes(t *testing.T, root string, names []string, preflight, updating bool, run func(string)) {
	t.Helper()
	if selected := os.Getenv("PLUGINS_CONTRACT_CASE"); selected != "" {
		require.Contains(t, names, selected)
		run(selected)
		return
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+root+"$", "-test.count=1",
				"-test.timeout=120s", "-update="+strconv.FormatBool(updating), "-plugins-preflight="+strconv.FormatBool(preflight))
			command.Env = append(os.Environ(), "PLUGINS_CONTRACT_CASE="+name, "TZ=UTC")
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			for line := range strings.SplitSeq(string(output), "\n") {
				if strings.HasPrefix(line, "TASKS_SNAPSHOT ") {
					fmt.Println(line)
				}
			}
		})
	}
}

func Updating(t *testing.T) bool {
	t.Helper()
	updating := flag.Lookup("update").Value.String() == "true"
	if updating {
		taskfixture.CheckRecording(t)
	}
	return updating
}

// State keeps persisted numbers and raw JSON strings intact, including private task fields.
func State(t *testing.T) map[string]any {
	t.Helper()
	state := taskfixture.State(t)
	state["plugins"] = taskfixture.Rows(t, model.DB, "task_plugins", "*", "id")
	state["abilities"] = taskfixture.Rows(t, model.DB, "abilities", "*", "channel_id,model")
	state["bindings"] = taskfixture.Rows(t, model.DB, "channels", "id,type,status,models,setting,other_info", "id")
	state["options"] = taskfixture.Rows(t, model.DB, "options", "*", "key")
	return state
}

type Outgoing struct {
	Method           string
	URL              string
	Headers          http.Header
	Host             string
	ContentLength    int64
	TransferEncoding []string
	Body             string
}

type Transport struct {
	T      *testing.T
	Calls  []Outgoing
	Status int
	Body   string
	Error  error
}

func (f *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	require.Equal(f.T, "plugins.invalid", request.URL.Host, "undeclared destination must never reach a socket")
	var raw []byte
	if request.Body != nil {
		var err error
		raw, err = io.ReadAll(request.Body)
		require.NoError(f.T, err)
		require.NoError(f.T, request.Body.Close())
	}
	f.Calls = append(f.Calls, Outgoing{request.Method, request.URL.String(), request.Header.Clone(), request.Host,
		request.ContentLength, request.TransferEncoding, string(raw)})
	if f.Error != nil {
		return nil, f.Error
	}
	status, body := f.Status, f.Body
	if status == 0 {
		status = 200
	}
	if body == "" {
		body = `{"id":"private-upstream-one","status":"IN_PROGRESS"}`
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

// CanonicalIDs verifies generated identity shapes and links before replacing exact occurrences.
func CanonicalIDs(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := common.Marshal(taskfixture.StableRequestIDs(t, value))
	require.NoError(t, err)
	if document, ok := value.(map[string]any); ok {
		if observations, ok := document["observations"].([]any); ok {
			for _, observation := range observations {
				headers := observation.(map[string]any)["headers"].(http.Header)
				identity := headers.Get(common.RequestIdKey)
				if identity == "" || identity == "billing-contract-request" {
					continue
				}
				require.Regexp(t, regexp.MustCompile(`^[0-9]{23}[A-Za-z0-9]{16}$`), identity)
				require.True(t, strings.HasPrefix(identity, common.GetTimeString()))
				encoded = []byte(strings.ReplaceAll(string(encoded), identity, "<http-request-id>"))
			}
		}
	}
	var tasks []model.Task
	require.NoError(t, model.DB.Order("id").Find(&tasks).Error)
	for _, task := range tasks {
		if !strings.HasPrefix(task.TaskID, "task_") || strings.HasPrefix(task.TaskID, "task_seed") {
			continue
		}
		old, replacement := taskfixture.BindTaskID(t, task.TaskID)
		// The Responses identity must share the durable task's generated suffix.
		responseID := "resp_" + strings.TrimPrefix(old, "task_")
		encoded = []byte(strings.ReplaceAll(string(encoded), responseID, "resp_<generated>"))
		encoded = []byte(strings.ReplaceAll(string(encoded), old, replacement))
	}
	return json.RawMessage(encoded)
}
