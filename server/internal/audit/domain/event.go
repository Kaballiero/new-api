package domain

const (
	AuditCategoryLogin       = "login"
	AuditCategorySecurity    = "security"
	AuditCategoryOperation   = "operation"
	AuditCategoryAccessToken = "access_token"
)

type Actor struct {
	ID       int
	Username string
	Role     int
}

type Roles struct {
	Common, Admin, Root int
}

func (roles Roles) Valid(role int) bool {
	return role == roles.Common || role == roles.Admin || role == roles.Root
}

type Operator struct {
	ID         int
	Username   string
	Role       int
	AuthMethod string
}

type RequestOutcome struct {
	Method, Route, Path string
	Status              int
	Success             bool
	Params              map[string]string
}

// Event contains audit facts independently of the stored metadata envelope.
type Event struct {
	EventID                                string
	OwnerID                                int
	Username                               string
	ActorRole                              int
	CreatedAt                              int64
	Category, Action, TokenRef, AuthMethod string
	IP, UserAgent, Method, Route           string
	Status                                 int
	Success                                bool
	RequestID, Content                     string
	OperationPresent                       bool
	OperationAction                        string
	Parameters                             map[string]any
	Operator                               *Operator
	Outcome                                *RequestOutcome
	RootFields                             map[string]any
	LoginMethod, LoginUserAgent            string
}

// RequestFacts is captured by transport. No Gin value or raw URL/body is retained.
type RequestFacts struct {
	Present                                             bool
	RequestID, IP, UserAgent, Method, Route, AuthMethod string
	Status                                              int
	RouteParams                                         map[string]string
}

type ResponseResult struct {
	Status           int
	Success          *bool
	BufferFull       bool
	HandlerSucceeded bool
}

func (result ResponseResult) Succeeded() bool {
	if result.Status >= 400 {
		return false
	}
	if result.Success != nil {
		return *result.Success
	}
	return true
}

func IsWriteMethod(method string) bool {
	return method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
}

// TokenAction selects observation before the handler, including nonempty status_only.
func TokenAction(method, route, statusOnly string) (string, string) {
	switch method + " " + route {
	case "POST /api/token/":
		return "token.create", "API token creation"
	case "PUT /api/token/":
		if statusOnly != "" {
			return "token.status_update", "API token status update"
		}
		return "token.update", "API token configuration update"
	case "DELETE /api/token/:id":
		return "token.delete", "API token deletion"
	case "POST /api/token/batch":
		return "token.delete_batch", "API token batch deletion"
	case "POST /api/token/:id/key":
		return "token.key_view", "API token key access"
	case "POST /api/token/batch/keys":
		return "token.key_view_batch", "API token batch key access"
	default:
		return "", ""
	}
}

func BoundBatchParameters(params map[string]any, ids []int) {
	params["total"] = len(ids)
	params["requested_ids"] = append([]int{}, ids[:min(len(ids), 100)]...)
	if len(ids) > 100 {
		params["requested_ids_truncated"] = true
	}
}
