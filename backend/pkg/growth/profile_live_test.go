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
		Name:          "Starfall Studio",
		Pitch:         " A fair MMORPG with no pay-to-win ",
		Games:         []string{"Starfall Online"},
		Genre:         "MMORPG",
		Competitors:   []string{"Rival Saga"},
		Regions:       []string{"SEA", " "},
		Languages:     []string{"英语", "泰语"},
		DefaultTarget: 50,
		Signals:       []string{"s1", "s2", "s3", "s4", "s5", "s6"},
		Excludes:      []string{"account sellers"},
		Notes:         "Players who quit Rival Saga after the last patch are the best fit.",
	}
}

func TestProfile_SaveProfile_WritesTheFileAndSeedsTheLists(t *testing.T) {
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
	assert.Equal(t, w.user, saved.ID)
	assert.NotNil(t, saved.UpdatedAt)
	assert.Equal(t, "A fair MMORPG with no pay-to-win", saved.Pitch)
	assert.Equal(t, []string{"SEA"}, saved.Regions)

	file, err := w.svc.db.GetUserResourceByPath(ctx, database.GetUserResourceByPathParams{UserID: w.user, Path: "business_profile.md"})
	require.NoError(t, err)
	body, err := os.ReadFile(resources.BlobPath(w.svc.cfg.DataDir, file.Hash))
	require.NoError(t, err)
	assert.Contains(t, string(body), "# Business profile: Starfall Studio")
	assert.Contains(t, string(body), "- s6")
	assert.Contains(t, string(body), "## Notes\n\nPlayers who quit Rival Saga")
	assert.Equal(t, int64(len(body)), file.Size)

	require.Len(t, k.docs, 3, "two lists plus the owner's own document")
	p := gameProfile()
	p.Competitors, p.Excludes = nil, nil
	_, err = w.svc.UpdateProfile(ctx, w.user, saved.ID, p)
	require.NoError(t, err)
	require.Len(t, k.docs, 2, "saving again replaces the profile's documents and keeps the rest")
	assert.Contains(t, k.docs, "mine")

	again, err := w.svc.db.GetUserResourceByPath(ctx, database.GetUserResourceByPathParams{UserID: w.user, Path: "business_profile.md"})
	require.NoError(t, err)
	assert.Equal(t, file.ID, again.ID)
	assert.NotEqual(t, file.Hash, again.Hash)

	_, err = w.svc.UpdateProfile(ctx, w.user, saved.ID+1, p)
	assert.ErrorIs(t, err, ErrNoProfile, "another owner's id")
}

func TestProfile_SaveProfile_KeepsTheProfileWhenKnowledgeIsUnavailable(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.svc.SetKnowledgeStore(&memoryKnowledge{docs: map[string]*model.KnowledgeDocument{}, failWith: errors.New("embedding provider is not configured")})

	list, err := w.svc.ListProfiles(ctx, w.user)
	require.NoError(t, err)
	assert.Empty(t, list)

	saved, err := w.svc.SaveProfile(ctx, w.user, gameProfile())
	require.NoError(t, err)
	assert.Equal(t, "skipped: embedding provider is not configured", saved.Knowledge)
	list, err = w.svc.ListProfiles(ctx, w.user)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "Starfall Studio", list[0].Name)

	_, err = w.svc.SaveProfile(ctx, w.user, Profile{Genre: "MMORPG"})
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
	assert.Contains(t, input, "Signal words: s1; s2; s3; s4; s5; …\n")
	assert.Contains(t, input, "Competitors (reject): Rival Saga\n")
	assert.Contains(t, input, "Exclude: account sellers\n")
	assert.Contains(t, input, "Notes: Players who quit Rival Saga after the last patch are the best fit.\n")
	assert.Contains(t, input, "Lead target: 50\n")
	assert.Contains(t, input, "Full profile: /work/resources/business_profile.md (the lines above are cut")
	require.Len(t, res, 1)
	assert.Equal(t, "business_profile.md", res[0].Path)

	p := gameProfile()
	p.DefaultTarget = 20
	p.Signals = p.Signals[:2]
	_, err = w.svc.SaveProfile(ctx, w.user, p)
	require.NoError(t, err)
	again, res := w.svc.WithProfile(ctx, w.user, input, res)
	assert.Equal(t, 1, strings.Count(again, "<business_profile>"))
	assert.Contains(t, again, "Lead target: 20\n")
	assert.Contains(t, again, "Full profile: /work/resources/business_profile.md\n", "nothing cut, no pointer")
	assert.True(t, strings.HasSuffix(again, "\n\nfind 50 players"))
	assert.Len(t, res, 1, "the file is attached once")
}

func TestProfile_SaveProfile_ChecksTheScoringAndTheContactTypes(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	saved, err := w.svc.SaveProfile(ctx, w.user, gameProfile())
	require.NoError(t, err)
	assert.Equal(t, []string{"platform_handle"}, saved.ContactTypes)
	assert.Equal(t, DefaultScoring, saved.ScoringOrDefault())

	tests := []struct {
		name    string
		edit    func(p *Profile)
		wantErr string
	}{
		{"an unknown contact type", func(p *Profile) { p.ContactTypes = []string{"fax"} }, `contact type "fax"`},
		{"a negative weight", func(p *Profile) {
			p.Scoring = &Scoring{Weights: ScoringWeights{Player: -1, Match: 9}, Qualified: 7, Watchlist: 4}
		}, "between 0 and 10"},
		{"a watchlist line at the qualified line", func(p *Profile) {
			p.Scoring = &Scoring{Weights: DefaultScoring.Weights, Qualified: 4, Watchlist: 4}
		}, "1 <= watchlist < qualified"},
		{"weights that never reach the qualified line", func(p *Profile) {
			p.Scoring = &Scoring{Weights: ScoringWeights{Player: 1, Match: 1}, Qualified: 7, Watchlist: 1}
		}, "no lead could qualify"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := gameProfile()
			tt.edit(&p)
			_, err := w.svc.SaveProfile(ctx, w.user, p)
			assert.ErrorIs(t, err, ErrInvalidProfile)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}

	p := gameProfile()
	p.ContactTypes = []string{" Phone", "email", "phone"}
	saved, err = w.svc.SaveProfile(ctx, w.user, p)
	require.NoError(t, err)
	assert.Equal(t, []string{"phone", "email"}, saved.ContactTypes)
	assert.Contains(t, saved.Compact(), "Contacts to find: phone, email")
}

func TestProfile_SaveProfile_DropsTheMessageScoresWhenTheAudienceChanges(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	src := w.mustAddTelegramGroup(t, w.seedTask(t), tgGroup("905", "rpg_chat", "RPG"))
	scored := func() int {
		rows, err := w.svc.db.GetMessageScores(ctx, database.GetMessageScoresParams{UserID: w.user, SourceID: src, MessageIds: []string{"m1"}})
		require.NoError(t, err)
		return len(rows)
	}
	_, err := w.svc.SaveProfile(ctx, w.user, gameProfile())
	require.NoError(t, err)
	require.NoError(t, w.svc.db.UpsertMessageScore(ctx, database.UpsertMessageScoreParams{
		UserID: w.user, SourceID: src, MessageID: "m1", Score: 8, Signals: "player+3",
	}))

	same := gameProfile()
	same.DefaultTarget = 80
	_, err = w.svc.SaveProfile(ctx, w.user, same)
	require.NoError(t, err)
	assert.Equal(t, 1, scored(), "a change the judgement model does not read keeps the scores")

	casino := gameProfile()
	casino.Pitch = "A licensed online casino"
	_, err = w.svc.SaveProfile(ctx, w.user, casino)
	require.NoError(t, err)
	assert.Equal(t, 0, scored())

	require.NoError(t, w.svc.db.UpsertMessageScore(ctx, database.UpsertMessageScoreParams{
		UserID: w.user, SourceID: src, MessageID: "m1", Score: 8, Signals: "player+3",
	}))
	casino.Scoring = &Scoring{Weights: ScoringWeights{Player: 1, Pain: 3, Paying: 3, Active: 2, Match: 3}, Qualified: 7, Watchlist: 4}
	_, err = w.svc.SaveProfile(ctx, w.user, casino)
	require.NoError(t, err)
	assert.Equal(t, 0, scored(), "new weights drop the scores too")
}
