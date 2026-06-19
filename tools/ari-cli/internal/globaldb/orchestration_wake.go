package globaldb

import (
	"context"
	"strings"
	"sync"
)

type storeAfterCommitKey struct{}

type storeAfterCommit struct {
	workspaceEventIDs map[string]struct{}
	schedulerWake     bool
}

type orchestrationWakeBroker struct {
	mu         sync.Mutex
	nextID     int64
	all        map[int64]chan struct{}
	workspaces map[string]map[int64]chan struct{}
}

func newOrchestrationWakeBroker() *orchestrationWakeBroker {
	return &orchestrationWakeBroker{all: map[int64]chan struct{}{}, workspaces: map[string]map[int64]chan struct{}{}}
}

func (s *Store) SubscribeOrchestrationWake() (<-chan struct{}, func()) {
	return s.orchestrationWake.subscribeAll()
}

func (s *Store) subscribeWorkspaceEventWake(workspaceID string) (<-chan struct{}, func()) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		closed := make(chan struct{})
		close(closed)
		return closed, func() {}
	}
	return s.orchestrationWake.subscribeWorkspace(workspaceID)
}

func (s *Store) notifyOrchestrationWake() {
	if s == nil || s.orchestrationWake == nil {
		return
	}
	s.orchestrationWake.notifyAll()
}

func (s *Store) notifyWorkspaceEventWake(workspaceID string) {
	if s == nil || s.orchestrationWake == nil {
		return
	}
	s.orchestrationWake.notifyWorkspace(workspaceID)
	s.orchestrationWake.notifyAll()
}

func recordWorkspaceEventAfterCommit(ctx context.Context, event WorkspaceEvent) {
	afterCommit, ok := ctx.Value(storeAfterCommitKey{}).(*storeAfterCommit)
	if !ok || afterCommit == nil {
		return
	}
	workspaceID := strings.TrimSpace(event.WorkspaceID)
	if workspaceID == "" {
		return
	}
	if afterCommit.workspaceEventIDs == nil {
		afterCommit.workspaceEventIDs = map[string]struct{}{}
	}
	afterCommit.workspaceEventIDs[workspaceID] = struct{}{}
	afterCommit.schedulerWake = true
}

func (s *Store) notifyAfterCommit(afterCommit *storeAfterCommit) {
	if afterCommit == nil {
		return
	}
	if afterCommit.schedulerWake {
		s.notifyOrchestrationWake()
	}
	for workspaceID := range afterCommit.workspaceEventIDs {
		s.notifyWorkspaceEventWake(workspaceID)
	}
}

func (b *orchestrationWakeBroker) subscribeAll() (<-chan struct{}, func()) {
	if b == nil {
		closed := make(chan struct{})
		close(closed)
		return closed, func() {}
	}
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	ch := make(chan struct{}, 1)
	b.all[id] = ch
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.all, id)
		b.mu.Unlock()
	}
}

func (b *orchestrationWakeBroker) subscribeWorkspace(workspaceID string) (<-chan struct{}, func()) {
	if b == nil {
		closed := make(chan struct{})
		close(closed)
		return closed, func() {}
	}
	workspaceID = strings.TrimSpace(workspaceID)
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	ch := make(chan struct{}, 1)
	workspaceSubs := b.workspaces[workspaceID]
	if workspaceSubs == nil {
		workspaceSubs = map[int64]chan struct{}{}
		b.workspaces[workspaceID] = workspaceSubs
	}
	workspaceSubs[id] = ch
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if workspaceSubs := b.workspaces[workspaceID]; workspaceSubs != nil {
			delete(workspaceSubs, id)
			if len(workspaceSubs) == 0 {
				delete(b.workspaces, workspaceID)
			}
		}
		b.mu.Unlock()
	}
}

func (b *orchestrationWakeBroker) notifyAll() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.all {
		notifyWakeSubscriber(ch)
	}
}

func (b *orchestrationWakeBroker) notifyWorkspace(workspaceID string) {
	if b == nil {
		return
	}
	workspaceID = strings.TrimSpace(workspaceID)
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.workspaces[workspaceID] {
		notifyWakeSubscriber(ch)
	}
}

func notifyWakeSubscriber(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
