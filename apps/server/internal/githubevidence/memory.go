package githubevidence

import (
	"sync"
)

type listedPage struct {
	kind       string
	repository int64
	page       int
	ids        []string
	repos      map[int64]string
}

type candidateMemory struct {
	mu         sync.Mutex
	pages      []*listedPage
	candidates map[string]candidate
}

func newCandidateMemory() *candidateMemory {
	return &candidateMemory{candidates: map[string]candidate{}}
}

func (m *candidateMemory) remember(kind string, repository int64, page int, items []candidate, ids []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.pages[:0]
	sameKind := 0
	for _, listed := range m.pages {
		if listed.kind == kind && listed.repository == repository && listed.page == page {
			m.forgetLocked(listed)
			continue
		}
		if listed.kind == kind {
			sameKind++
		}
		kept = append(kept, listed)
	}
	m.pages = kept
	if sameKind >= maxListingPages {
		for i, listed := range m.pages {
			if listed.kind == kind {
				m.forgetLocked(listed)
				m.pages = append(m.pages[:i], m.pages[i+1:]...)
				break
			}
		}
	}
	listed := &listedPage{kind: kind, repository: repository, page: page, ids: ids}
	if kind == kindRepository {
		listed.repos = make(map[int64]string, len(items))
	}
	for i, item := range items {
		m.candidates[ids[i]] = item
		if listed.repos != nil {
			listed.repos[item.repository.id] = item.repository.fullName
		}
	}
	m.pages = append(m.pages, listed)
}

func (m *candidateMemory) forgetLocked(listed *listedPage) {
	for _, id := range listed.ids {
		delete(m.candidates, id)
	}
}

func (m *candidateMemory) candidate(id string) (candidate, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.candidates[id]
	return item, ok
}

func (m *candidateMemory) repository(id int64) (candidateRepository, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.pages) - 1; i >= 0; i-- {
		if fullName, ok := m.pages[i].repos[id]; ok {
			return candidateRepository{id: id, fullName: fullName}, true
		}
	}
	return candidateRepository{}, false
}
