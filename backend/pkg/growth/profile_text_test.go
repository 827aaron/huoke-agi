package growth

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileText_ParseScoring_RoundTripsTheDefaultAndTheShortLines(t *testing.T) {
	text := DefaultScoringText()
	sc, pen, err := ParseScoring(text)
	require.NoError(t, err)
	assert.Equal(t, DefaultScoring(), sc)
	assert.Equal(t, DefaultUnwantedPenalty, pen)
	assert.Equal(t, text, FormatScoring(sc, pen))

	short := "# comment\nplayer = 3\npain = 3 | unhappy with what they use\npaying = 2\nmatch = 3\nrecent = 2\nqualified >= 7\nwatchlist >= 4\nunwanted = -3\n"
	sc, pen, err = ParseScoring(short)
	require.NoError(t, err)
	assert.Equal(t, 3, pen)
	assert.Equal(t, "player", sc.Dimensions[0].Key)
	assert.Equal(t, "Audience", sc.Dimensions[0].Name, "a bare built-in key keeps Jev's question")
	assert.Equal(t, "unhappy with what they use", sc.Dimensions[1].Description)
	assert.Equal(t, 7, sc.Qualified)
	assert.Equal(t, 4, sc.Watchlist)
}

func TestProfileText_ParseScoring_RejectsALineThatCannotScore(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"not a line", "player = 3\nhello", "not a scoring line"},
		{"no signal", "qualified >= 7\nwatchlist >= 4", "at least one dimension"},
		{"watchlist too high", "player = 3\nqualified >= 4\nwatchlist >= 4", "watchlist < qualified"},
		{"unreachable", "player = 1\nqualified >= 7\nwatchlist >= 1", "no lead could qualify"},
		{"points", "player = 21\nqualified >= 7\nwatchlist >= 1", "between 0 and 20"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseScoring(tt.text)
			require.ErrorIs(t, err, ErrInvalidProfile)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func sampleProfile() Profile {
	return Profile{
		Name: "Starfall Studio", Pitch: " A fair MMORPG with no pay-to-win ",
		Games: []string{"Starfall Online"}, Genre: "MMORPG", Competitors: []string{"Rival Saga"},
		Regions: []string{"SEA", " "}, Languages: []string{"英语", "泰语"}, DefaultTarget: 50,
		Signals: []string{"s1", "s2", "s3", "s4", "s5", "s6"}, Unwanted: []string{"account sellers"},
		Notes: "Players who quit Rival Saga after the last patch are the best fit.",
	}
}

func TestProfileText_Fold_RejectsACategoryWithNothingToLookFor(t *testing.T) {
	p := Profile{Genre: "MMORPG"}
	p.normalize()
	assert.ErrorIs(t, p.validate(), ErrInvalidProfile)
}

func TestProfileText_Fold_CopiesTheOldFieldsIntoTheThreePrompts(t *testing.T) {
	p := sampleProfile()
	p.normalize()
	assert.Contains(t, p.Need, "Offer: A fair MMORPG with no pay-to-win")
	assert.Contains(t, p.Need, "Signal words: s1; s2; s3; s4; s5; s6")
	assert.Contains(t, p.Need, "Lead target: 50")
	assert.Equal(t, "Competitor: Rival Saga\naccount sellers", p.Exclude)
	assert.Contains(t, string(p.ScoringText), "qualified >= 7")
	assert.Equal(t, []string{"SEA"}, p.Regions)
	assert.Equal(t, []string{"s1", "s2", "s3", "s4", "s5", "s6"}, p.Signals)

	block := p.compact("", false)
	assert.Contains(t, block, "## Need\n")
	assert.Contains(t, block, "## Exclude\n")
	assert.Contains(t, block, "## Jev score\n")
	assert.Contains(t, block, "Always excluded, on explicit evidence only:")
}

func TestProfileText_Apply_EditsOnePromptAndKeepsTheRest(t *testing.T) {
	p := sampleProfile()
	p.normalize()
	before := p.Need

	require.NoError(t, p.apply(ProfileOp{Action: "append", Field: "exclude", Text: "people in Singapore"}))
	assert.Equal(t, before, p.Need)
	assert.True(t, strings.HasSuffix(p.Exclude, "people in Singapore"))

	require.NoError(t, p.apply(ProfileOp{Action: "add_section", SectionTitle: "预算", Text: "只找愿意付费的"}))
	require.NoError(t, p.apply(ProfileOp{Action: "set", SectionTitle: "预算", NewTitle: "付费意愿", Text: "提到充值的优先"}))
	require.Len(t, p.Sections, 1)
	assert.Equal(t, "付费意愿", p.Sections[0].Title)
	assert.Equal(t, "提到充值的优先", p.Sections[0].Body)

	require.NoError(t, p.apply(ProfileOp{Action: "remove_section", SectionID: p.Sections[0].ID}))
	assert.Empty(t, p.Sections)

	err := p.apply(ProfileOp{Action: "set", Field: "scoring", Text: "player = 3\nqualified >= 4\nwatchlist >= 4"})
	require.NoError(t, err)
	p.normalize()
	assert.ErrorContains(t, p.validate(), "watchlist < qualified")
}

func TestProfileText_Unmarshal_ReadsAScoringObjectAsText(t *testing.T) {
	var p Profile
	raw := []byte(`{"name":"Old","excludes":["a stated age under 18","affiliates promoting other platforms"],` +
		`"scoring":{"weights":{"player":3,"pain":3,"paying":3,"active":2,"match":3},"qualified":8,"watchlist":4}}`)
	require.NoError(t, json.Unmarshal(raw, &p))
	p.normalize()
	assert.Equal(t, []string{"affiliates promoting other platforms"}, p.Unwanted)
	require.NotNil(t, p.Scoring)
	assert.Equal(t, DefaultScoring().Dimensions[0].Key, p.Scoring.Dimensions[0].Key)
	assert.Equal(t, 8, p.Scoring.Qualified)
	assert.Equal(t, 2, p.Scoring.Recent.Points)
	out, err := json.Marshal(p)
	require.NoError(t, err)
	assert.NotContains(t, string(out), `"weights"`)
	assert.NotContains(t, string(out), `"excludes"`)
	assert.Contains(t, string(out), `"scoring":"player = 3`)
}

func TestProfileText_WithPrompts_FillsTheThreePromptsOfAPreset(t *testing.T) {
	for _, preset := range ProfilePresets() {
		p := preset.Profile.WithPrompts()
		assert.NotEmpty(t, p.Need)
		assert.NotEmpty(t, p.Exclude)
		assert.Contains(t, string(p.ScoringText), "qualified >=")
		assert.Contains(t, string(p.ScoringText), "watchlist >=")
	}
	casino := ProfilePresets()[0].Profile.WithPrompts()
	assert.Contains(t, casino.Need, "Offer: A licensed")
	assert.Contains(t, casino.Exclude, "bots and spam")
	assert.Contains(t, string(casino.ScoringText), "qualified >= 8")
	assert.Contains(t, string(casino.ScoringText), "player = 3 | Audience |")
}
