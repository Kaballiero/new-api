package complete_oauth

type Result struct {
	Issue                                 string
	Cause                                 error
	Message                               string
	BindingSucceeded, NotificationWarning bool
}
