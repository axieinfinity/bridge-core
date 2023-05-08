package types

import "math"

type Pool[T any] interface {
	// Pop an item out of pool
	Get() T
	// Push an item into pool
	Put(T)
	// Get an item, executes a function on this item then puts it back
	Do(f func(T) error) error
	// Return amount of remaining items inside pool
	Size() int
	// Return the maximum of items that can be stored inside pool
	Capacity() int
}

type pool[T any] struct {
	queue chan T

	capacity int
}

func (p *pool[T]) Get() T {
	v := <-p.queue
	return v
}

func (p *pool[T]) Put(item T) {
	if len(p.queue) >= p.capacity {
		return
	}

	p.queue <- item
}

func (p *pool[T]) Do(f func(T) error) error {
	var (
		item T
		err  error
	)

	item = p.Get()
	err = f(item)

	p.Put(item)
	return err
}

func (p *pool[T]) Size() int {
	return len(p.queue)
}

func (p *pool[T]) Capacity() int {
	return p.capacity
}

func NewPool[T any](items []T, capacity int) Pool[T] {
	p := &pool[T]{
		queue:    make(chan T, capacity),
		capacity: capacity,
	}
	count := int(math.Min(float64(capacity), float64(len(items))))
	for i := 0; i < count; i++ {
		p.Put(items[i])
	}

	return p
}
