package test_channels

type Task struct {
	ID           string
	Status, Type string
}
type Output struct {
	Task    Task
	Created bool
}
