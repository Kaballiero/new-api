package channels

import "github.com/QuantumNous/new-api/internal/channels/test_channels"

type TestQueue struct {
	enqueue func() (test_channels.Task, bool, error)
}

func NewTestQueue(enqueue func() (test_channels.Task, bool, error)) TestQueue {
	return TestQueue{enqueue: enqueue}
}
func (queue TestQueue) EnqueueChannelTest() (test_channels.Task, bool, error) { return queue.enqueue() }
