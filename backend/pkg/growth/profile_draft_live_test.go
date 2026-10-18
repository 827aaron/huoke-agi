//go:build postgres

package growth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileDraft_ApplyProfileDraft_SavesTheDraftAndBindsTheTask(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	base := w.mustCreateProfile(t, "game", gameProfile())

	_, err := w.svc.ProfileDraft(ctx, w.user, scope.FlowID)
	assert.ErrorIs(t, err, ErrNoProfileDraft)
	_, err = w.svc.SaveProfileDraft(ctx, scope, "", nil, Profile{Genre: "MMORPG"})
	assert.ErrorIs(t, err, ErrInvalidProfile, "a draft is checked as a save is")
	_, err = w.svc.SaveProfileDraft(ctx, Scope{UserID: w.user}, "", nil, gameProfile())
	assert.ErrorIs(t, err, ErrFlowNotFound)

	agents := Profile{Name: "MY agents", Signals: []string{"agent wanted"}}
	d, err := w.svc.SaveProfileDraft(ctx, scope, "agents", &base.ID, agents)
	require.NoError(t, err)
	assert.Equal(t, &base.ID, d.BaseProfileID)
	got, err := w.svc.ProfileDraft(ctx, w.user, scope.FlowID)
	require.NoError(t, err)
	assert.Equal(t, "agents", got.Title)
	assert.Equal(t, []string{"agent wanted"}, got.Profile.Signals)

	other := seedUser(t, w.db, "other@example.com")
	_, err = w.svc.ProfileDraft(ctx, other, scope.FlowID)
	assert.ErrorIs(t, err, ErrNoProfileDraft, "another owner's task")

	saved, err := w.svc.ApplyProfileDraft(ctx, w.user, scope.FlowID, ApplyDraftOptions{AsNew: true, UseForFlow: true})
	require.NoError(t, err)
	assert.NotEqual(t, base.ID, saved.ID, "as_new keeps the base profile")
	assert.False(t, saved.Default)
	fp, err := w.svc.FlowProfile(ctx, w.user, scope.FlowID)
	require.NoError(t, err)
	assert.Equal(t, FlowProfileView{FlowID: scope.FlowID, ProfileID: &saved.ID, Title: "agents"}, fp)
	_, err = w.svc.ProfileDraft(ctx, w.user, scope.FlowID)
	assert.ErrorIs(t, err, ErrNoProfileDraft, "a saved draft is gone")
	b, err := w.svc.GetProfile(ctx, w.user, base.ID)
	require.NoError(t, err)
	assert.Equal(t, "Starfall Studio", b.Name)

	_, err = w.svc.SaveProfileDraft(ctx, scope, "agents v2", &base.ID, agents)
	require.NoError(t, err)
	over, err := w.svc.ApplyProfileDraft(ctx, w.user, scope.FlowID, ApplyDraftOptions{Title: " agents MY ", MakeDefault: true})
	require.NoError(t, err)
	assert.Equal(t, base.ID, over.ID, "without as_new the draft saves over its base")
	assert.Equal(t, "agents MY", over.Title, "a title given on save replaces the draft's")
	assert.Equal(t, "MY agents", over.Name)
	assert.True(t, over.Default)
	fp, err = w.svc.FlowProfile(ctx, w.user, scope.FlowID)
	require.NoError(t, err)
	assert.Equal(t, &saved.ID, fp.ProfileID, "without use_for_flow the task keeps its profile")

	_, err = w.svc.SaveProfileDraft(ctx, scope, "", nil, agents)
	require.NoError(t, err)
	require.NoError(t, w.svc.DiscardProfileDraft(ctx, w.user, scope.FlowID))
	assert.ErrorIs(t, w.svc.DiscardProfileDraft(ctx, w.user, scope.FlowID), ErrNoProfileDraft)
	_, err = w.svc.ApplyProfileDraft(ctx, w.user, scope.FlowID, ApplyDraftOptions{})
	assert.ErrorIs(t, err, ErrNoProfileDraft)
}
