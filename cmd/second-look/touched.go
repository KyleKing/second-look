package main

import (
	"sync"

	"github.com/kyleking/second-look/internal/snapshot"
)

// touched collects the pull requests a sitting changed on the forge, from the
// seams that reach it, so the queue stops painting what it knew of them before.
// A nil touched collects nothing.
type touched struct {
	mu  sync.Mutex
	prs []place
}

func (t *touched) add(repository string, number int) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.prs = append(t.prs, place{repo: repository, number: number})
}

// addRef is add for a reference whose owner and repository are held apart.
func (t *touched) addRef(owner, repo string, number int) {
	t.add(owner+"/"+repo, number)
}

// evict forgets everything collected so far in snaps.
func (t *touched) evict(snaps *snapshot.Store) {
	if t == nil || snaps == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, p := range t.prs {
		snaps.Evict(p.repo, p.number)
	}

	t.prs = nil
}
