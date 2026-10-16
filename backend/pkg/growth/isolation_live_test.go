//go:build postgres

package growth

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pentagi/pkg/growth/connectors"
)

// anotherCustomer is a second user of the same server with an account of its own.
func (w *world) anotherCustomer(t *testing.T, mail string) *world {
	t.Helper()
	other := &world{db: w.db, svc: w.svc, tg: NewMemoryConnector(connectors.PlatformTelegram), user: seedUser(t, w.db, mail)}
	acc, err := w.svc.EnsureAccount(context.Background(), other.user, AccountSpec{Platform: connectors.PlatformTelegram})
	require.NoError(t, err)
	w.svc.SetConnector(acc.ID, other.tg)
	return other
}

func TestLeads_UpsertLeads_SamePersonForTwoCustomersIsTwoLeads(t *testing.T) {
	a := newWorld(t)
	b := a.anotherCustomer(t, "b@example.test")
	ctx := context.Background()
	scopeA, scopeB := a.seedTask(t), b.seedTask(t)
	srcA := a.mustAddTelegramGroup(t, scopeA, tgGroup("910", "shared_group", "Shared"))
	srcB := b.mustAddTelegramGroup(t, scopeB, tgGroup("910", "shared_group", "Shared"))
	assert.NotEqual(t, srcA, srcB, "each customer has a pool row of their own for the same group")

	resA, err := a.svc.UpsertLeads(ctx, scopeA, []LeadInput{qualified("42", srcA, "m1")})
	require.NoError(t, err)
	resB, err := b.svc.UpsertLeads(ctx, scopeB, []LeadInput{qualified("42", srcB, "m1")})
	require.NoError(t, err)
	assert.Equal(t, LeadStatusNew, resA[0].Status)
	assert.Equal(t, LeadStatusNew, resB[0].Status, "another customer's lead is not a duplicate")

	leadsA, err := a.svc.ListLeads(ctx, a.user, LeadFilter{})
	require.NoError(t, err)
	leadsB, err := b.svc.ListLeads(ctx, b.user, LeadFilter{})
	require.NoError(t, err)
	require.Len(t, leadsA, 1)
	require.Len(t, leadsB, 1)
	assert.NotEqual(t, leadsA[0].ID, leadsB[0].ID)
}

func TestRunner_Deliver_OneCustomersDeliveryLeavesTheOthersLeadOpen(t *testing.T) {
	a := newWorld(t)
	b := a.anotherCustomer(t, "b@example.test")
	ctx := context.Background()
	scopeA, scopeB := a.seedTask(t), b.seedTask(t)
	srcA := a.mustAddTelegramGroup(t, scopeA, tgGroup("911", "rpg_a", "RPG"))
	srcB := b.mustAddTelegramGroup(t, scopeB, tgGroup("911", "rpg_a", "RPG"))
	_, err := a.svc.SetTarget(ctx, scopeA, 1)
	require.NoError(t, err)
	a.mustSaveQualified(t, scopeA, srcA, 1, 1)
	d, err := a.svc.Deliver(ctx, scopeA, false)
	require.NoError(t, err)
	require.Equal(t, RunStatusCompleted, d.Status)

	res, err := b.svc.UpsertLeads(ctx, scopeB, []LeadInput{qualified("1", srcB, "m1")})
	require.NoError(t, err)
	assert.False(t, res[0].AlreadyDeliver, "a person delivered to one customer is still new to the other")

	undelivered := false
	open, err := b.svc.ListLeads(ctx, b.user, LeadFilter{Delivered: &undelivered})
	require.NoError(t, err)
	assert.Len(t, open, 1)
}

func TestSources_DeleteSource_RefusesAnotherCustomersSource(t *testing.T) {
	a := newWorld(t)
	b := a.anotherCustomer(t, "b@example.test")
	ctx := context.Background()
	scopeA := a.seedTask(t)
	srcA := a.mustAddTelegramGroup(t, scopeA, tgGroup("912", "private_pool", "Pool"))

	err := b.svc.DeleteSource(ctx, b.user, srcA)
	assert.ErrorIs(t, err, ErrSourceNotFound)

	listedByB, err := b.svc.ListSources(ctx, b.user)
	require.NoError(t, err)
	assert.Empty(t, listedByB)
	listedByA, err := a.svc.ListSources(ctx, a.user)
	require.NoError(t, err)
	assert.Len(t, listedByA, 1)
}

func TestSettings_SaveSetting_OneCustomersKeyIsInvisibleToTheOther(t *testing.T) {
	a := newWorld(t)
	b := a.anotherCustomer(t, "b@example.test")
	ctx := context.Background()

	_, err := a.svc.SaveSetting(ctx, a.user, SettingYouTube, "", map[string]string{"api_keys": "yt-secret-AAAA1111"})
	require.NoError(t, err)

	views, err := b.svc.ListSettings(ctx, b.user)
	require.NoError(t, err)
	for _, v := range views {
		if v.Kind == SettingYouTube {
			assert.Equal(t, SettingSourceNone, v.Source)
			assert.Empty(t, v.Fields["api_keys"])
		}
	}
}

func TestLeads_Delete_FlowAndTaskLeaveLeadsAndSourcesInPlace(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, scope, tgGroup("913", "keep_me", "Keep"))
	w.mustSaveQualified(t, scope, src, 1, 2)

	_, err := w.db.Exec(`DELETE FROM tasks WHERE id = $1`, *scope.TaskID)
	require.NoError(t, err)
	_, err = w.db.Exec(`DELETE FROM flows WHERE id = $1`, scope.FlowID)
	require.NoError(t, err)

	leads, err := w.svc.ListLeads(ctx, w.user, LeadFilter{})
	require.NoError(t, err)
	require.Len(t, leads, 2, "deleting the flow keeps its leads")
	for _, l := range leads {
		assert.Nil(t, l.FlowID)
		assert.Nil(t, l.TaskID)
		assert.NotEmpty(t, l.Evidence, "evidence stays with the lead")
	}

	pool, err := w.svc.ListSources(ctx, w.user)
	require.NoError(t, err)
	require.Len(t, pool, 1, "deleting the flow keeps the pool")

	var firstFlow, lastFlow, evidenceFlow sql.NullInt64
	require.NoError(t, w.db.QueryRow(`SELECT first_flow_id, last_flow_id FROM leads WHERE user_id = $1 LIMIT 1`, w.user).
		Scan(&firstFlow, &lastFlow))
	assert.False(t, firstFlow.Valid)
	assert.False(t, lastFlow.Valid)
	require.NoError(t, w.db.QueryRow(`SELECT flow_id FROM lead_evidence LIMIT 1`).Scan(&evidenceFlow))
	assert.False(t, evidenceFlow.Valid)
}

func TestLeads_Schema_RefusesASecondRowForTheSamePerson(t *testing.T) {
	w := newWorld(t)
	scope := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, scope, tgGroup("914", "dup_guard", "Dup"))
	w.mustSaveQualified(t, scope, src, 1, 1)

	_, err := w.db.Exec(`
		INSERT INTO leads (user_id, platform, platform_user_id, identity_kind, score, tier)
		SELECT user_id, platform, platform_user_id, identity_kind, score, tier FROM leads WHERE user_id = $1 LIMIT 1`, w.user)

	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr), "want a unique violation, got %v", err)
	assert.Equal(t, "leads_user_platform_user_unique", pqErr.Constraint)
}
