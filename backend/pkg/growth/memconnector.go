package growth

import (
	"context"
	"slices"
	"strings"
	"sync"

	"pentagi/pkg/growth/connectors"
)

// MemoryConnector is an in-process stand-in for a platform: a fixed set of sources,
// their messages and users. It lets the tools and the round logic run end to end
// before a real connector exists, and is what tests bind with Service.SetConnector.
type MemoryConnector struct {
	PlatformName connectors.Platform
	Caps         []connectors.Capability

	mu       sync.Mutex
	sources  []connectors.Source
	messages map[string][]connectors.Message
	users    map[string]connectors.UserProfile
	joins    map[string]connectors.JoinResult
	// Err, when set, is returned by every call; tests use it for rate limits and lost sessions.
	Err    error
	Joined map[string]int
}

func NewMemoryConnector(platform connectors.Platform) *MemoryConnector {
	return &MemoryConnector{
		PlatformName: platform,
		Caps: []connectors.Capability{
			connectors.CapSearchSources, connectors.CapResolveSource, connectors.CapRecommendSources,
			connectors.CapJoinSource, connectors.CapReadHistory, connectors.CapUserProfile,
		},
		messages: map[string][]connectors.Message{},
		users:    map[string]connectors.UserProfile{},
		joins:    map[string]connectors.JoinResult{},
		Joined:   map[string]int{},
	}
}

// AddSource registers a source; messages are given oldest first.
func (m *MemoryConnector) AddSource(src connectors.Source, msgs ...connectors.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sources = append(m.sources, src)
	m.messages[src.ExternalID] = append(m.messages[src.ExternalID], msgs...)
}

func (m *MemoryConnector) AddUser(u connectors.UserProfile) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[u.ID] = u
}

// SetJoinResult overrides the joined outcome of one source.
func (m *MemoryConnector) SetJoinResult(externalID string, res connectors.JoinResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.joins[externalID] = res
}

func (m *MemoryConnector) Platform() connectors.Platform         { return m.PlatformName }
func (m *MemoryConnector) Capabilities() []connectors.Capability { return m.Caps }
func (m *MemoryConnector) Close() error                          { return nil }

func (m *MemoryConnector) SearchSources(ctx context.Context, query string, limit int) ([]connectors.Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var out []connectors.Source
	for _, s := range m.sources {
		if strings.Contains(strings.ToLower(s.Title+" "+s.Username+" "+s.Description), q) {
			out = append(out, s)
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (m *MemoryConnector) ResolveSource(ctx context.Context, link string) (connectors.Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return connectors.Source{}, err
	}
	canonical, err := CanonicalLink(m.PlatformName, link)
	if err != nil {
		return connectors.Source{}, connectors.ErrNotFound
	}
	for _, s := range m.sources {
		if c, err := CanonicalLink(m.PlatformName, s.Link); err == nil && c == canonical {
			return s, nil
		}
		if s.Username != "" && "t.me/"+strings.ToLower(s.Username) == canonical {
			return s, nil
		}
	}
	return connectors.Source{}, connectors.ErrNotFound
}

func (m *MemoryConnector) RecommendSources(ctx context.Context, ref connectors.SourceRef, limit int) ([]connectors.Source, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return nil, err
	}
	var out []connectors.Source
	for _, s := range m.sources {
		if s.ExternalID != ref.ExternalID {
			out = append(out, s)
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (m *MemoryConnector) JoinSource(ctx context.Context, ref connectors.SourceRef) (connectors.JoinResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return connectors.JoinResult{}, err
	}
	m.Joined[ref.ExternalID]++
	if res, ok := m.joins[ref.ExternalID]; ok {
		return res, nil
	}
	return connectors.JoinResult{Status: connectors.JoinStatusJoined}, nil
}

func (m *MemoryConnector) ReadHistory(
	ctx context.Context,
	ref connectors.SourceRef,
	req connectors.HistoryRequest,
) (connectors.HistoryPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return connectors.HistoryPage{}, err
	}
	all, ok := m.messages[ref.ExternalID]
	if !ok {
		return connectors.HistoryPage{}, connectors.ErrNotFound
	}

	var page []connectors.Message
	if req.MinID > 0 && req.OffsetID == 0 {
		for _, msg := range all {
			if msg.ID > req.MinID && len(page) < req.Limit {
				page = append(page, msg)
			}
		}
		slices.Reverse(page)
		return connectors.HistoryPage{Messages: page}, nil
	}
	for i := len(all) - 1; i >= 0 && len(page) < req.Limit; i-- {
		msg := all[i]
		if req.OffsetID != 0 && msg.ID >= req.OffsetID {
			continue
		}
		if msg.ID <= req.MinID {
			continue
		}
		page = append(page, msg)
	}

	exhausted := req.MinID == 0 && (len(page) == 0 ||
		!slices.ContainsFunc(all, func(x connectors.Message) bool { return x.ID < page[len(page)-1].ID }))
	return connectors.HistoryPage{Messages: page, Exhausted: exhausted}, nil
}

func (m *MemoryConnector) GetUser(ctx context.Context, userID string) (connectors.UserProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx); err != nil {
		return connectors.UserProfile{}, err
	}
	u, ok := m.users[userID]
	if !ok {
		return connectors.UserProfile{}, connectors.ErrNotFound
	}
	return u, nil
}

func (m *MemoryConnector) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.Err
}
