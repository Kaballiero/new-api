package detect_all_upstream_model_updates

type Task struct{ ID, Status, Type string }
type Output struct {
	Task    Task
	Created bool
}
