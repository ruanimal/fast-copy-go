package copyengine

import (
	"context"
	"sync"
)

// Gate suspends a task at the next I/O boundary. Cancellation always wakes a
// paused task; system calls already in progress are allowed to finish.
type Gate struct {
	mu     sync.Mutex
	paused chan struct{}
}

func (g *Gate) Pause() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused == nil {
		g.paused = make(chan struct{})
	}
}
func (g *Gate) Resume() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused != nil {
		close(g.paused)
		g.paused = nil
	}
}
func (g *Gate) Wait(ctx context.Context) error {
	g.mu.Lock()
	ch := g.paused
	g.mu.Unlock()
	if ch == nil {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return ctx.Err()
	}
}
func waitForTask(ctx context.Context, opt Options) error {
	if opt.Gate != nil {
		return opt.Gate.Wait(ctx)
	}
	return ctx.Err()
}
