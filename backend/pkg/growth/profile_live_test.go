//go:build postgres

package growth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
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

func (w *world) mustCreateProfile(t *testing.T, title string, p Profile) ProfileView {
	t.Helper()
	v, err := w.svc.CreateProfile(context.Background(), w.user, ProfileInput{Title: title, Profile: p})
	require.NoError(t, err)
	return v
}

func profileDocs(k *memoryKnowledge) []string {
	var out []string
	for _, d := range k.docs {
		if d.Description != nil && *d.Description == profileGuideMarker {
			out = append(out, d.Content)
		}
	}
	slices.Sort(out)
	return out
}

func TestProfile_CreateProfile_SeedsOnlyTheDefaultProfilesLists(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	k := &memoryKnowledge{docs: map[string]*model.KnowledgeDocument{}}
	keep := "my own note"
	k.docs["mine"] = &model.KnowledgeDocument{ID: "mine", UserID: w.user, Description: &keep}
	w.svc.SetKnowledgeStore(k)

	first, err := w.svc.CreateProfile(ctx, w.user, ProfileInput{Profile: gameProfile()})
	require.NoError(t, err)
	assert.True(t, first.Default, "a customer's first profile is the default")
	assert.Equal(t, "Starfall Studio", first.Title, "an empty title is the business name")
	assert.Equal(t, "seeded", first.Knowledge)
	assert.NotNil(t, first.UpdatedAt)
	assert.Equal(t, "A fair MMORPG with no pay-to-win", first.Pitch)
	assert.Equal(t, []string{"SEA"}, first.Regions)
	assert.Equal(t, []string{"- Competitor: Rival Saga\n- account sellers", "- s1\n- s2\n- s3\n- s4\n- s5\n- s6"}, profileDocs(k))

	agents := gameProfile()
	agents.Signals, agents.Competitors, agents.Unwanted = []string{"agent wanted"}, nil, nil
	second, err := w.svc.CreateProfile(ctx, w.user, ProfileInput{Title: " MY agents ", Profile: agents})
	require.NoError(t, err)
	assert.False(t, second.Default)
	assert.Equal(t, "MY agents", second.Title)
	assert.Empty(t, second.Knowledge)
	assert.Len(t, profileDocs(k), 2, "a profile that is not the default leaves the store alone")

	again, err := w.svc.SetDefaultProfile(ctx, w.user, second.ID)
	require.NoError(t, err)
	assert.True(t, again.Default)
	assert.Equal(t, []string{"- agent wanted"}, profileDocs(k))
	assert.Contains(t, k.docs, "mine")

	list, err := w.svc.ListProfiles(ctx, w.user)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, []int64{second.ID, first.ID}, []int64{list[0].ID, list[1].ID}, "the default first")
	assert.False(t, list[1].Default)

	other := seedUser(t, w.db, "other@example.com")
	_, err = w.svc.UpdateProfile(ctx, other, first.ID, ProfileInput{Profile: gameProfile()})
	assert.ErrorIs(t, err, ErrNoProfile, "another owner's id")
	_, err = w.svc.SetDefaultProfile(ctx, other, first.ID)
	assert.ErrorIs(t, err, ErrNoProfile)
	assert.ErrorIs(t, w.svc.DeleteProfile(ctx, other, first.ID), ErrNoProfile)
}

func TestProfile_CreateProfile_KeepsTheProfileWhenKnowledgeIsUnavailable(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.svc.SetKnowledgeStore(&memoryKnowledge{docs: map[string]*model.KnowledgeDocument{}, failWith: errors.New("embedding provider is not configured")})

	list, err := w.svc.ListProfiles(ctx, w.user)
	require.NoError(t, err)
	assert.Empty(t, list)

	saved, err := w.svc.CreateProfile(ctx, w.user, ProfileInput{Profile: gameProfile()})
	require.NoError(t, err)
	assert.Equal(t, "skipped: embedding provider is not configured", saved.Knowledge)
	list, err = w.svc.ListProfiles(ctx, w.user)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "Starfall Studio", list[0].Name)

	_, err = w.svc.CreateProfile(ctx, w.user, ProfileInput{Profile: Profile{Genre: "MMORPG"}})
	assert.ErrorIs(t, err, ErrInvalidProfile)
}

func TestProfile_DeleteProfile_MakesTheOldestRemainingOneTheDefault(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	k := &memoryKnowledge{docs: map[string]*model.KnowledgeDocument{}}
	w.svc.SetKnowledgeStore(k)
	a := w.mustCreateProfile(t, "a", gameProfile())
	b := w.mustCreateProfile(t, "b", Profile{Name: "B", Signals: []string{"b words"}})
	c := w.mustCreateProfile(t, "c", Profile{Name: "C"})
	flow := w.seedTask(t).FlowID
	require.NoError(t, w.svc.BindFlowProfile(ctx, flow, a))

	require.NoError(t, w.svc.DeleteProfile(ctx, w.user, c.ID))
	got, err := w.svc.DefaultProfileOf(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, a.ID, got.ID, "deleting another profile keeps the default")

	require.NoError(t, w.svc.DeleteProfile(ctx, w.user, a.ID))
	got, err = w.svc.DefaultProfileOf(ctx, w.user)
	require.NoError(t, err)
	assert.Equal(t, b.ID, got.ID)
	assert.Equal(t, []string{"- b words"}, profileDocs(k))

	fp, err := w.svc.FlowProfile(ctx, w.user, flow)
	require.NoError(t, err)
	assert.Equal(t, FlowProfileView{FlowID: flow, ProfileID: &b.ID, Title: "b", Fallback: true}, fp,
		"a flow whose profile is deleted uses the default")

	require.NoError(t, w.svc.DeleteProfile(ctx, w.user, b.ID))
	assert.Empty(t, profileDocs(k))
	v, err := w.svc.ProfileForFlow(ctx, w.user, flow)
	require.NoError(t, err)
	assert.Equal(t, int64(0), v.ID)
	assert.Equal(t, DefaultProfile().Pitch, v.Pitch, "with no profile left the built-in one stands in")
	assert.ErrorIs(t, w.svc.DeleteProfile(ctx, w.user, b.ID), ErrNoProfile)
}

func TestProfile_ProfileForFlow_KeepsTheProfileAFlowStartedWith(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	a := w.mustCreateProfile(t, "a", gameProfile())
	b := w.mustCreateProfile(t, "b", Profile{Name: "B"})
	bound, unbound := w.seedTask(t).FlowID, w.seedTask(t).FlowID

	v, err := w.svc.ResolveProfile(ctx, w.user, &b.ID)
	require.NoError(t, err)
	require.NoError(t, w.svc.BindFlowProfile(ctx, bound, v))
	_, err = w.svc.SetDefaultProfile(ctx, w.user, b.ID)
	require.NoError(t, err)
	_, err = w.svc.SetDefaultProfile(ctx, w.user, a.ID)
	require.NoError(t, err)

	got, err := w.svc.ProfileForFlow(ctx, w.user, bound)
	require.NoError(t, err)
	assert.Equal(t, b.ID, got.ID, "the default changing does not move a started flow")
	got, err = w.svc.ProfileForFlow(ctx, w.user, unbound)
	require.NoError(t, err)
	assert.Equal(t, a.ID, got.ID, "a flow with no record uses the default")

	fp, err := w.svc.SetFlowProfile(ctx, w.user, unbound, b.ID)
	require.NoError(t, err)
	assert.Equal(t, FlowProfileView{FlowID: unbound, ProfileID: &b.ID, Title: "b"}, fp)

	other := seedUser(t, w.db, "other@example.com")
	_, err = w.svc.ResolveProfile(ctx, other, &a.ID)
	assert.ErrorIs(t, err, ErrNoProfile, "a chosen profile must be the owner's")
	_, err = w.svc.SetFlowProfile(ctx, other, unbound, a.ID)
	assert.ErrorIs(t, err, ErrFlowNotFound)
}

func TestProfile_ProfileInput_PutsTheFlowsCompactBlockFirstOnce(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.svc.cfg.DataDir = t.TempDir()

	builtIn, err := w.svc.ResolveProfile(ctx, w.user, nil)
	require.NoError(t, err)
	input, res := w.svc.ProfileInput(ctx, w.user, builtIn, "find 50 players", nil)
	require.True(t, strings.HasPrefix(input, "<business_profile>\n## Need\nOffer: A licensed, regulated online gaming operator"), input)
	assert.Contains(t, input, "## Jev score\nplayer = 3 | Audience |")
	assert.Contains(t, input, "qualified >= 8\nwatchlist >= 4")
	assert.Contains(t, input, "Always excluded, on explicit evidence only: a stated age under 18; "+
		"an explicit request for help to stop gambling, or a stated self-exclusion\n")
	assert.Contains(t, input, "Full profile: /work/resources/business_profile.md (read the file, or search_guide")
	assert.True(t, strings.HasSuffix(input, "</business_profile>\n\nfind 50 players"), input)
	require.Len(t, res, 1)
	assert.Equal(t, "business_profile.md", res[0].Path)

	game := w.mustCreateProfile(t, "game", gameProfile())
	second := w.mustCreateProfile(t, "second", gameProfile())
	input, res = w.svc.ProfileInput(ctx, w.user, game, "find 50 players", []database.UserResource{{Path: "notes.md"}})
	require.True(t, strings.HasPrefix(input, "<business_profile>\nName: Starfall Studio\n"), input)
	assert.Contains(t, input, "Signal words: s1; s2; s3; s4; s5; s6\n")
	assert.Contains(t, input, "Competitor: Rival Saga\n")
	assert.Contains(t, input, "account sellers\n")
	assert.Contains(t, input, "Lead target: 50\n")
	assert.Contains(t, input, "qualified >= 7\nwatchlist >= 4")
	fileName := fmt.Sprintf("business_profile_%d.md", game.ID)
	assert.Contains(t, input, "Full profile: /work/resources/"+fileName+" (the lines above are cut; read the file, or search_guide")
	require.Len(t, res, 2)
	assert.Equal(t, fileName, res[1].Path)
	body, err := os.ReadFile(resources.BlobPath(w.svc.cfg.DataDir, res[1].Hash))
	require.NoError(t, err)
	assert.Contains(t, string(body), "# Business profile: Starfall Studio")
	assert.Contains(t, string(body), "## Exclude\nCompetitor: Rival Saga\naccount sellers")
	assert.Contains(t, string(body), "player = 3 | Audience | The sender uses or follows")
	assert.Equal(t, int64(len(body)), res[1].Size)

	again, _ := w.svc.ProfileInput(ctx, w.user, second, input, nil)
	assert.Equal(t, 1, strings.Count(again, "<business_profile>"))
	assert.NotContains(t, again, "search_guide", "only the default profile's lists are in the knowledge store")
	assert.True(t, strings.HasSuffix(again, "\n\nfind 50 players"))
}

func TestProfile_UpdateProfile_ChecksTheScoringAndTheContactTypes(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	saved := w.mustCreateProfile(t, "game", gameProfile())
	assert.Equal(t, []string{"platform_handle"}, saved.ContactTypes)
	assert.Equal(t, DefaultScoring(), saved.ScoringOrDefault())

	dims := func(points ...int) []ScoringDimension {
		out := []ScoringDimension{}
		for i, p := range points {
			out = append(out, ScoringDimension{Name: fmt.Sprintf("d%d", i), Points: p})
		}
		return out
	}
	tests := []struct {
		name    string
		edit    func(p *Profile)
		wantErr string
	}{
		{"a contact type over 30 characters", func(p *Profile) { p.ContactTypes = []string{strings.Repeat("x", 31)} }, "longer than 30"},
		{"no dimension", func(p *Profile) { p.Scoring = &Scoring{Qualified: 7, Watchlist: 4} }, "at least one dimension"},
		{"a dimension without a name", func(p *Profile) {
			p.Scoring = &Scoring{Dimensions: []ScoringDimension{{Description: "asks for odds", Points: 9}}, Qualified: 7, Watchlist: 4}
		}, "needs a name"},
		{"negative points", func(p *Profile) {
			p.Scoring = &Scoring{Dimensions: dims(-1, 9), Qualified: 7, Watchlist: 4}
		}, "between 0 and 20"},
		{"a watchlist line at the qualified line", func(p *Profile) {
			p.Scoring = &Scoring{Dimensions: dims(5, 5), Qualified: 4, Watchlist: 4}
		}, "1 <= watchlist < qualified"},
		{"points that never reach the qualified line", func(p *Profile) {
			p.Scoring = &Scoring{Dimensions: dims(1, 1), Recent: &RecentDimension{Points: 2}, Qualified: 7, Watchlist: 1}
		}, "no lead could qualify"},
		{"a negative unwanted penalty", func(p *Profile) { p.UnwantedPenalty = -2 }, "unwanted_penalty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := gameProfile()
			tt.edit(&p)
			_, err := w.svc.UpdateProfile(ctx, w.user, saved.ID, ProfileInput{Profile: p})
			assert.ErrorIs(t, err, ErrInvalidProfile)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}

	p := gameProfile()
	p.ContactTypes = []string{" Phone", "email", "phone"}
	p.Scoring = &Scoring{
		Dimensions: []ScoringDimension{
			{Name: "Agent experience", Description: "Has run a betting agency", Points: 5},
			{Key: "Agent Experience", Name: "Downline", Points: 3},
			{Name: "下线", Points: 2},
			{Name: " ", Description: " "},
		},
		Qualified: 7, Watchlist: 3,
	}
	saved, err := w.svc.UpdateProfile(ctx, w.user, saved.ID, ProfileInput{Title: "agents", Profile: p})
	require.NoError(t, err)
	assert.Equal(t, "agents", saved.Title)
	assert.Equal(t, []string{"phone", "email"}, saved.ContactTypes)
	assert.Equal(t, []ScoringDimension{
		{Key: "agent_experience", Name: "Agent experience", Description: "Has run a betting agency", Points: 5},
		{Key: "d2", Name: "Downline", Points: 3},
		{Key: "d3", Name: "下线", Points: 2},
	}, saved.Scoring.Dimensions, "keys are unique slugs, an empty dimension is dropped")
	assert.Contains(t, saved.Compact(), "Contacts to find: phone, email")
	assert.Contains(t, saved.Compact(), "agent_experience = 5 | Agent experience |")
	assert.Contains(t, saved.Compact(), "qualified >= 7")

	p.ContactTypes = []string{"LINE", "messenger", " WeChat", "wechat"}
	saved, err = w.svc.UpdateProfile(ctx, w.user, saved.ID, ProfileInput{Profile: p})
	require.NoError(t, err)
	assert.Equal(t, []string{"line", "whatsapp", "telegram", "WeChat"}, saved.ContactTypes)
	assert.Equal(t, "Starfall Studio", saved.Title, "an empty title is the business name")
}

func TestProfile_GetProfile_ReadsAProfileStoredInTheOlderForm(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var id int64
	require.NoError(t, w.db.QueryRow(`INSERT INTO growth_profiles (user_id, title, profile, is_default) VALUES ($1, 'old', $2, true) RETURNING id`,
		w.user, `{"name":"Old","excludes":["a stated age under 18","an explicit request for help to stop gambling, or a stated self-exclusion",`+
			`"affiliates promoting other platforms","未成年用户"],"scoring":{"weights":{"player":3,"pain":3,"paying":3,"active":2,"match":3},"qualified":8,"watchlist":4}}`,
	).Scan(&id))

	v, err := w.svc.GetProfile(ctx, w.user, id)
	require.NoError(t, err)
	assert.Equal(t, []string{"affiliates promoting other platforms"}, v.Unwanted, "the compliance lines move to the system")
	assert.Nil(t, v.LegacyExcludes)
	require.NotNil(t, v.Scoring)
	assert.Equal(t, Scoring{
		Dimensions: defaultDimensions(3, 3, 3, 3), Recent: &RecentDimension{Points: 2}, Qualified: 8, Watchlist: 4,
	}, *v.Scoring, "the five weights become the same dimensions")
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"excludes"`)
	assert.NotContains(t, string(raw), `"weights"`)
}

func TestProfile_UpdateProfile_DropsTheMessageScoresOfThatProfileWhenTheAudienceChanges(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	src := w.mustAddTelegramGroup(t, w.seedTask(t), tgGroup("905", "rpg_chat", "RPG"))
	game := w.mustCreateProfile(t, "game", gameProfile())
	other := w.mustCreateProfile(t, "other", gameProfile())
	scored := func(profileID int64) int {
		rows, err := w.svc.db.GetMessageScores(ctx, database.GetMessageScoresParams{
			UserID: w.user, ProfileID: profileID, SourceID: src, MessageIds: []string{"m1"},
		})
		require.NoError(t, err)
		return len(rows)
	}
	score := func(profileID int64) {
		require.NoError(t, w.svc.db.UpsertMessageScore(ctx, database.UpsertMessageScoreParams{
			UserID: w.user, ProfileID: profileID, SourceID: src, MessageID: "m1", Score: 8, Signals: "player+3",
		}))
	}
	score(game.ID)
	score(other.ID)

	_, err := w.svc.UpdateProfile(ctx, w.user, game.ID, ProfileInput{Title: "renamed", Profile: gameProfile()})
	require.NoError(t, err)
	assert.Equal(t, 1, scored(game.ID), "renaming the profile does not change what leads are judged by")

	casino := gameProfile()
	casino.Pitch = "A licensed online casino"
	_, err = w.svc.UpdateProfile(ctx, w.user, game.ID, ProfileInput{Profile: casino})
	require.NoError(t, err)
	assert.Equal(t, 0, scored(game.ID))
	assert.Equal(t, 1, scored(other.ID), "another profile's scores stay")

	score(game.ID)
	casino.Scoring = &Scoring{Dimensions: defaultDimensions(1, 3, 3, 3), Recent: &RecentDimension{Points: 2}, Qualified: 7, Watchlist: 4}
	_, err = w.svc.UpdateProfile(ctx, w.user, game.ID, ProfileInput{Profile: casino})
	require.NoError(t, err)
	assert.Equal(t, 0, scored(game.ID), "new points drop the scores too")

	score(game.ID)
	casino.UnwantedPenalty = 5
	_, err = w.svc.UpdateProfile(ctx, w.user, game.ID, ProfileInput{Profile: casino})
	require.NoError(t, err)
	assert.Equal(t, 0, scored(game.ID), "so does a new unwanted penalty")

	score(game.ID)
	casino.Unwanted = append(casino.Unwanted, "people in Singapore")
	_, err = w.svc.UpdateProfile(ctx, w.user, game.ID, ProfileInput{Profile: casino})
	require.NoError(t, err)
	assert.Equal(t, 0, scored(game.ID), "and a new unwanted entry, which the judgement model is asked about")

	require.NoError(t, w.svc.DeleteProfile(ctx, w.user, other.ID))
	assert.Equal(t, 0, scored(other.ID), "a deleted profile takes its scores along")
}

var errProfileRead = errors.New("connection reset")

type failingProfileRead struct{ database.Querier }

func (failingProfileRead) GetUserGrowthProfile(context.Context, database.GetUserGrowthProfileParams) (database.GrowthProfile, error) {
	return database.GrowthProfile{}, errProfileRead
}

func TestProfile_UpdateProfile_DropsScoresOnlyWhenThePreviousProfileWasRead(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	src := w.mustAddTelegramGroup(t, w.seedTask(t), tgGroup("906", "rpg_chat", "RPG"))
	game := w.mustCreateProfile(t, "game", gameProfile())
	require.NoError(t, w.svc.db.UpsertMessageScore(ctx, database.UpsertMessageScoreParams{
		UserID: w.user, ProfileID: game.ID, SourceID: src, MessageID: "m1", Score: 8, Signals: "player+3",
	}))
	scored := func() int {
		rows, err := w.svc.db.GetMessageScores(ctx, database.GetMessageScoresParams{
			UserID: w.user, ProfileID: game.ID, SourceID: src, MessageIds: []string{"m1"},
		})
		require.NoError(t, err)
		return len(rows)
	}

	db := w.svc.db
	w.svc.db = failingProfileRead{db}
	_, err := w.svc.UpdateProfile(ctx, w.user, game.ID, ProfileInput{Profile: DefaultProfile()})
	w.svc.db = db
	require.ErrorIs(t, err, errProfileRead)
	assert.Equal(t, 1, scored(), "a failed read keeps the scores")

	_, err = w.db.Exec(`UPDATE growth_profiles SET profile = '"unreadable"' WHERE id = $1`, game.ID)
	require.NoError(t, err)
	_, err = w.svc.UpdateProfile(ctx, w.user, game.ID, ProfileInput{Profile: gameProfile()})
	require.NoError(t, err, "an unreadable profile can be saved over")
	assert.Equal(t, 0, scored(), "it was judged as the default, which differs")
}
