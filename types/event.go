package types

import "context"

type EventStreaming struct {
	listeners map[string]Listener
}

func (p *EventStreaming) Publish(ctx context.Context, event string, message []byte) error {
	// distribute events to listeners
	p.listeners[event].
	return nil
}

func (p *EventStreaming) pick(event string) Listener {

}

func NewEventStore(listeners map[string]Listener) *EventStreaming {
	return &EventStreaming{
		listeners: listeners,
	}
}
