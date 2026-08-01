package dgo

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	gatewaySendLimit      = 120
	gatewaySendWindow     = 60 * time.Second
	gatewayWriteQueueSize = gatewaySendLimit + 1
)

var errGatewayWriteQueueClosed = errors.New("gateway write queue is closed")

type gatewayJSONWriter interface {
	WriteJSON(interface{}) error
}

type gatewayWriteRequest struct {
	ctx    context.Context
	data   interface{}
	result chan error
}

// gatewayWriteQueue is the only data-writer for one Gateway websocket
// generation. Keeping the rate window in the writer makes all Gateway
// operations share the same accounting, regardless of which public helper
// submitted them.
type gatewayWriteQueue struct {
	ctx    context.Context
	writer gatewayJSONWriter
	mu     *sync.Mutex
	clock  identifyClock

	requests chan gatewayWriteRequest
	done     chan struct{}
}

func newGatewayWriteQueue(
	ctx context.Context,
	writer gatewayJSONWriter,
	mu *sync.Mutex,
	clock identifyClock,
) *gatewayWriteQueue {
	if ctx == nil {
		ctx = context.Background()
	}
	if clock == nil {
		clock = realIdentifyClock{}
	}
	return &gatewayWriteQueue{
		ctx:      ctx,
		writer:   writer,
		mu:       mu,
		clock:    clock,
		requests: make(chan gatewayWriteRequest, gatewayWriteQueueSize),
		done:     make(chan struct{}),
	}
}

func (q *gatewayWriteQueue) enqueue(ctx context.Context, data interface{}) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	request := gatewayWriteRequest{
		ctx:    ctx,
		data:   data,
		result: make(chan error, 1),
	}
	select {
	case q.requests <- request:
	case <-q.done:
		return errGatewayWriteQueueClosed
	case <-q.ctx.Done():
		return q.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-request.result:
		return err
	case <-q.done:
		return errGatewayWriteQueueClosed
	case <-q.ctx.Done():
		return q.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *gatewayWriteQueue) run() {
	defer close(q.done)

	if q.writer == nil || q.mu == nil {
		q.failPending(errors.New("gateway write queue has no writer"))
		return
	}

	sentAt := make([]time.Time, 0, gatewaySendLimit)
	for {
		select {
		case <-q.ctx.Done():
			q.failPending(q.ctx.Err())
			return
		case request := <-q.requests:
			if err := request.ctx.Err(); err != nil {
				request.result <- err
				continue
			}

			var err error
			sentAt, err = q.waitForSlot(sentAt)
			if err != nil {
				request.result <- err
				continue
			}

			q.mu.Lock()
			err = q.writer.WriteJSON(request.data)
			q.mu.Unlock()
			if err == nil {
				sentAt = append(sentAt, q.clock.Now())
			}
			request.result <- err
		}
	}
}

func (q *gatewayWriteQueue) waitForSlot(sentAt []time.Time) ([]time.Time, error) {
	for {
		now := q.clock.Now()
		cutoff := now.Add(-gatewaySendWindow)
		firstActive := 0
		for firstActive < len(sentAt) && !sentAt[firstActive].After(cutoff) {
			firstActive++
		}
		if firstActive > 0 {
			sentAt = append(sentAt[:0], sentAt[firstActive:]...)
		}
		if len(sentAt) < gatewaySendLimit {
			return sentAt, nil
		}

		delay := sentAt[0].Add(gatewaySendWindow).Sub(now)
		if delay <= 0 {
			continue
		}
		if err := waitGatewayWrite(q.ctx, q.clock, delay); err != nil {
			return sentAt, err
		}
	}
}

func waitGatewayWrite(ctx context.Context, clock identifyClock, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return clock.Wait(ctx, delay)
}

func (q *gatewayWriteQueue) failPending(err error) {
	for {
		select {
		case request := <-q.requests:
			request.result <- err
		default:
			return
		}
	}
}
