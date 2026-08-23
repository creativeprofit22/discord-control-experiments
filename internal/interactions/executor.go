package interactions

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

type WorkItem struct {
	InteractionID string
	CorrelationID domain.CorrelationID
	TraceContext  context.Context
	Run           func(context.Context)
}
type Executor struct {
	capacity int
	workers  int
	timeout  time.Duration
	mu       sync.RWMutex
	queue    chan WorkItem
	slots    chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	started  atomic.Bool
}

type WorkReservation struct {
	executor *Executor
	queue    chan WorkItem
	slots    chan struct{}
	done     atomic.Bool
}

func NewExecutor(capacity, workers int, timeout time.Duration) (*Executor, error) {
	if capacity < 1 || workers < 1 || timeout <= 0 || timeout > 15*time.Minute {
		return nil, errors.New("invalid executor options")
	}
	return &Executor{capacity: capacity, workers: workers, timeout: timeout}, nil
}
func (e *Executor) Start(parent context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.started.CompareAndSwap(false, true) {
		return errors.New("executor already started")
	}
	e.queue = make(chan WorkItem, e.capacity)
	e.slots = make(chan struct{}, e.capacity)
	e.ctx, e.cancel = context.WithCancel(parent)
	for range e.workers {
		e.wg.Add(1)
		go e.worker(e.ctx, e.queue, e.slots)
	}
	return nil
}
func (e *Executor) Reserve() *WorkReservation {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.started.Load() {
		return nil
	}
	select {
	case e.slots <- struct{}{}:
		return &WorkReservation{executor: e, queue: e.queue, slots: e.slots}
	default:
		return nil
	}
}
func (reservation *WorkReservation) Commit(item WorkItem) bool {
	if reservation == nil || item.InteractionID == "" || item.Run == nil || !reservation.done.CompareAndSwap(false, true) {
		return false
	}
	reservation.executor.mu.RLock()
	defer reservation.executor.mu.RUnlock()
	if !reservation.executor.started.Load() || reservation.executor.queue != reservation.queue {
		reservation.releaseSlot()
		return false
	}
	select {
	case reservation.queue <- item:
		return true
	default:
		reservation.releaseSlot()
		return false
	}
}
func (reservation *WorkReservation) Release() {
	if reservation != nil && reservation.done.CompareAndSwap(false, true) {
		reservation.releaseSlot()
	}
}
func (reservation *WorkReservation) releaseSlot() {
	select {
	case <-reservation.slots:
	default:
	}
}
func (e *Executor) Enqueue(item WorkItem) bool {
	reservation := e.Reserve()
	return reservation != nil && reservation.Commit(item)
}
func (e *Executor) Stop() {
	e.mu.Lock()
	if !e.started.CompareAndSwap(true, false) {
		e.mu.Unlock()
		return
	}
	cancel := e.cancel
	queue := e.queue
	slots := e.slots
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	e.wg.Wait()
	for {
		select {
		case <-queue:
			select {
			case <-slots:
			default:
			}
		default:
			return
		}
	}
}
func (e *Executor) Depth() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.started.Load() {
		return 0
	}
	return len(e.queue)
}
func (e *Executor) worker(ctx context.Context, queue <-chan WorkItem, slots chan struct{}) {
	defer e.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-queue:
			<-slots
			traceContext := item.TraceContext
			if traceContext == nil {
				traceContext = ctx
			}
			workCtx, cancel := context.WithTimeout(context.WithoutCancel(traceContext), e.timeout)
			stopExecutorCancellation := context.AfterFunc(ctx, cancel)
			item.Run(workCtx)
			stopExecutorCancellation()
			cancel()
		}
	}
}
