//go:build postgres

package growth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"pentagi/pkg/database"
	"pentagi/pkg/graph/model"
	"pentagi/pkg/resources"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryKnowledge keeps guide documents per owner, failing every call once failWith is set.
type memoryKnowledge struct {
	docs     map[string]*model.KnowledgeDocument
	next     int
	failWith error
}

func (m *memoryKnowledge) ListUserDocuments(ctx context.Context, userID int64, _ *model.KnowledgeFilter, _ bool) ([]*model.KnowledgeDocument, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []*model.KnowledgeDocument
	for _, d := range m.docs {
		if d.UserID == userID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (m *memoryKnowledge) CreateDocument(ctx context.Context, userID int64, in model.CreateKnowledgeDocumentInput) (*model.KnowledgeDocument, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.failWith != nil {
		return nil, m.failWith
	}
	m.next++
	d := &model.KnowledgeDocument{ID: fmt.Sprint(m.next), UserID: userID, DocType: in.DocType, Question: in.Question,
		Content: in.Content, Description: in.Description}
	m.docs[d.ID] = d
	return d, nil
}

func (m *memoryKnowledge) DeleteUserDocument(ctx context.Context, userID int64, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d, ok := m.docs[id]; ok && d.UserID == userID {
		delete(m.docs, id)
	}
	return nil
}

func gameProfile() Profile {
	return Profile{
		BusinessName: "Starfall Studio",
		Product:      " Starfall Online ",
		Audience:     "MMORPG players tired of pay-to-win",
		Regions:      []string{"SEA", " "},
		Languages:    []string{"en", "th"},
		TargetLeads:  50,
		Signals:      []string{"s1", "s2", "s3", "s4", "s5", "s6"},
		Sources:      []string{"https://t.me/mmo_hub"},
		SignalWords:  []string{"looking for a new mmo", "quit my game"},
		Exclusions:   []string{"@rival_studio"},
	}
}

func TestProfile_SaveProfile_WritesTheFileAndSeedsTheLongLists(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.svc.cfg.DataDir = t.TempDir()
	k := &memoryKnowledge{docs: map[string]*model.KnowledgeDocument{}}
	keep := "my own note"
	k.docs["mine"] = &model.KnowledgeDocument{ID: "mine", UserID: w.user, Description: &keep}
	w.svc.SetKnowledgeStore(k)

	saved, err := w.svc.SaveProfile(ctx, w.user, gameProfile())
	require.NoError(t, err)
	assert.Equal(t, "seeded", saved.Knowledge)
	assert.Equal(t, "Starfall Online", saved.Profile.Product)
	assert.Equal(t, []string{"SEA"}, saved.Profile.Regions)

	file, err := w.svc.db.GetUserResourceByPath(ctx, database.GetUserResourceByPathParams{UserID: w.user, Path: "business_profile.md"})
	require.NoError(t, err)
	body, err := os.ReadFile(resources.BlobPath(w.svc.cfg.DataDir, file.Hash))
	require.NoError(t, err)
	assert.Contains(t, string(body), "# Business profile: Starfall Studio")
	assert.NotContains(t, string(body), "## Bad-actor samples", "an empty list has no section")
	assert.Contains(t, string(body), "- quit my game")
	assert.Equal(t, int64(len(body)), file.Size)

	require.Len(t, k.docs, 3, "two lists plus the owner's own document")
	p := gameProfile()
	p.SignalWords = []string{"returning player"}
	p.Exclusions = nil
	_, err = w.svc.SaveProfile(ctx, w.user, p)
	require.NoError(t, err)
	require.Len(t, k.docs, 2, "saving again replaces the profile's documents and keeps the rest")
	assert.Contains(t, k.docs, "mine")

	again, err := w.svc.db.GetUserResourceByPath(ctx, database.GetUserResourceByPathParams{UserID: w.user, Path: "business_profile.md"})
	require.NoError(t, err)
	assert.Equal(t, file.ID, again.ID)
	assert.NotEqual(t, file.Hash, again.Hash)
}

func TestProfile_SaveProfile_KeepsTheProfileWhenKnowledgeIsUnavailable(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.svc.SetKnowledgeStore(&memoryKnowledge{docs: map[string]*model.KnowledgeDocument{}, failWith: errors.New("embedding provider is not configured")})

	saved, err := w.svc.SaveProfile(ctx, w.user, gameProfile())
	require.NoError(t, err)
	assert.Equal(t, "skipped: embedding provider is not configured", saved.Knowledge)
	got, err := w.svc.GetProfile(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, "Starfall Studio", got.BusinessName)

	_, err = w.svc.SaveProfile(ctx, w.user, Profile{Audience: "players"})
	assert.ErrorIs(t, err, ErrInvalidProfile)
}

func TestProfile_WithProfile_PutsTheCompactBlockFirstOnce(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.svc.cfg.DataDir = t.TempDir()

	input, res := w.svc.WithProfile(ctx, w.user, "find 50 players", nil)
	assert.Equal(t, "find 50 players", input, "no profile, no change")
	assert.Empty(t, res)

	_, err := w.svc.SaveProfile(ctx, w.user, gameProfile())
	require.NoError(t, err)
	input, res = w.svc.WithProfile(ctx, w.user, "find 50 players", nil)
	require.True(t, strings.HasPrefix(input, "<business_profile>\nBusiness: Starfall Studio\n"), input)
	assert.True(t, strings.HasSuffix(input, "</business_profile>\n\nfind 50 players"), input)
	assert.Contains(t, input, "Custom signals: s1; s2; s3; s4; s5\n")
	assert.Contains(t, input, "Lead target: 50\n")
	assert.Contains(t, input, "Full profile: /work/resources/business_profile.md; signal words")
	assert.NotContains(t, input, "quit my game", "long lists stay out of the input")
	require.Len(t, res, 1)
	assert.Equal(t, "business_profile.md", res[0].Path)

	p := gameProfile()
	p.TargetLeads = 20
	_, err = w.svc.SaveProfile(ctx, w.user, p)
	require.NoError(t, err)
	again, res := w.svc.WithProfile(ctx, w.user, input, res)
	assert.Equal(t, 1, strings.Count(again, "<business_profile>"))
	assert.Contains(t, again, "Lead target: 20\n")
	assert.True(t, strings.HasSuffix(again, "\n\nfind 50 players"))
	assert.Len(t, res, 1, "the file is attached once")
}
