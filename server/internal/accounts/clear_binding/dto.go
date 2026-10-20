package clear_binding

type Input struct {
	ID          int
	BindingType string
}
type Result struct {
	Forbidden bool
	Cause     error
}
