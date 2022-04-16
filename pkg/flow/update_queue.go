package flow

import (
	"context"
	"sync"
)

// updateQueue is an unordered queue of updated components to be processed.
type updateQueue struct {
	updated  sync.Map
	updateCh chan struct{}
}

func newUpdateQueue() *updateQueue {
	return &updateQueue{
		updateCh: make(chan struct{}, 1),
	}
}

// Enqueue enqueues a new componentNode to be dequeued later.
func (uq *updateQueue) Enqueue(cn *componentNode) {
	uq.updated.Store(cn, struct{}{})

	select {
	case uq.updateCh <- struct{}{}:
	default:
	}
}

// Dequeue dequeues a componentNode from the queue. If the queue is empty,
// Dequeue blocks until there is an element to dequeue or until ctx is
// canceled.
func (uq *updateQueue) Dequeue(ctx context.Context) (*componentNode, error) {
	// Try to dequeue immediately if there's something in the queue.
	if elem := uq.dequeue(); elem != nil {
		return elem, nil
	}

	// Otherwise, wait for updateCh to be readable.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-uq.updateCh:
		return uq.dequeue(), nil
	}
}

func (uq *updateQueue) dequeue() *componentNode {
	var res *componentNode

	uq.updated.Range(func(key, _ any) bool {
		res = key.(*componentNode)
		uq.updated.Delete(key)
		return false
	})

	return res
}