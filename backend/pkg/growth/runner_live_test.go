//go:build postgres

package growth

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunner_Deliver_RefusesShortOfTheTargetAndDeliversNothing(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, scope, tgGroup("700", "arpg", "ARPG"))
	_, err := w.svc.SetTarget(ctx, scope, 3)
	require.NoError(t, err)
	w.mustSaveQualified(t, scope, src, 1, 2)

	d, err := w.svc.Deliver(ctx, scope, DeliverOptions{})
	require.NoError(t, err)
	assert.Equal(t, DeliveryIncomplete, d.Status)
	assert.Equal(t, 1, d.Gap)
	assert.Empty(t, d.Leads)
	assert.Contains(t, d.Instruction, "1 more qualified leads")
	assert.Equal(t, "PROGRESS: new_leads_this_task=2 target=3", d.Progress.Line())

	undelivered := false
	left, err := w.svc.ListLeads(ctx, w.user, LeadFilter{Delivered: &undelivered})
	require.NoError(t, err)
	assert.Len(t, left, 2)
}

func TestRunner_Deliver_CompletesAtTheTargetWithTheBestLeads(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, scope, tgGroup("701", "mmo_hub", "MMO"))
	_, err := w.svc.SetTarget(ctx, scope, 2)
	require.NoError(t, err)
	w.mustSaveQualified(t, scope, src, 1, 2)
	best := qualified("99", src, "m99")
	recent := time.Now().UTC()
	best.Evidence[0].MessageTime = &recent
	_, err = w.svc.UpsertLeads(ctx, scope, []LeadInput{best})
	require.NoError(t, err)

	d, err := w.svc.Deliver(ctx, scope, DeliverOptions{})
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, d.Status)
	require.Len(t, d.Leads, 2)
	assert.Equal(t, "99", d.Leads[0].PlatformUserID)
	assert.Equal(t, "skipped: no SMTP server is configured", d.Email)

	again, err := w.svc.Deliver(ctx, scope, DeliverOptions{})
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, again.Status)
	assert.Len(t, again.Leads, 2)
	assert.Empty(t, again.Email, "a repeated call delivers nothing new and mails nothing")
}

func TestRunner_Deliver_PartialOnlyWhenAllowedAndTopsUpLater(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, scope, tgGroup("702", "rts_hub", "RTS"))
	_, err := w.svc.SetTarget(ctx, scope, 3)
	require.NoError(t, err)
	w.mustSaveQualified(t, scope, src, 1, 1)

	d, err := w.svc.Deliver(ctx, scope, DeliverOptions{AllowPartial: true})
	require.NoError(t, err)
	assert.Equal(t, RunStatusPartial, d.Status)
	assert.Equal(t, 2, d.Gap)
	assert.Len(t, d.Leads, 1)

	w.mustSaveQualified(t, scope, src, 2, 2)
	d, err = w.svc.Deliver(ctx, scope, DeliverOptions{})
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, d.Status)
	assert.Len(t, d.Leads, 3)
}

func TestRunner_Deliver_NeverDeliversAPersonTwiceEvenAfterTheFlowIsDeleted(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	first := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, first, tgGroup("703", "moba", "MOBA"))
	_, err := w.svc.SetTarget(ctx, first, 2)
	require.NoError(t, err)
	w.mustSaveQualified(t, first, src, 1, 2)
	d, err := w.svc.Deliver(ctx, first, DeliverOptions{})
	require.NoError(t, err)
	require.Equal(t, RunStatusCompleted, d.Status)

	_, err = w.db.Exec(`DELETE FROM flows WHERE id = $1`, first.FlowID)
	require.NoError(t, err)

	second := w.seedTask(t)
	_, err = w.svc.SetTarget(ctx, second, 2)
	require.NoError(t, err)
	res, err := w.svc.UpsertLeads(ctx, second, []LeadInput{
		qualified("1", src, "again-1"), qualified("2", src, "again-2"), qualified("3", src, "m3"),
	})
	require.NoError(t, err)
	assert.True(t, res[0].AlreadyDeliver)
	assert.True(t, res[1].AlreadyDeliver)
	assert.False(t, res[2].AlreadyDeliver)

	p, err := w.svc.Progress(ctx, second)
	require.NoError(t, err)
	assert.Equal(t, 1, p.NewLeads, "people delivered by the deleted flow do not count again")

	d, err = w.svc.Deliver(ctx, second, DeliverOptions{AllowPartial: true})
	require.NoError(t, err)
	require.Len(t, d.Leads, 1)
	assert.Equal(t, "3", d.Leads[0].PlatformUserID)

	runs, err := w.svc.ListRuns(ctx, w.user, nil)
	require.NoError(t, err)
	assert.Len(t, runs, 2, "the deleted flow's round is kept")
}

func TestRunner_Deliver_MailsTheListWithTheCSVAttached(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	sink := startSMTPSink(t)
	w.saveSink(t, sink, "boss@example.test, ops@example.test")
	scope := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, scope, tgGroup("704", "gacha", "Gacha"))
	_, err := w.svc.SetTarget(ctx, scope, 1)
	require.NoError(t, err)
	w.mustSaveQualified(t, scope, src, 1, 1)

	d, err := w.svc.Deliver(ctx, scope, DeliverOptions{})
	require.NoError(t, err)
	assert.Equal(t, NotifySent, d.Email)

	msgs := sink.Messages()
	require.Len(t, msgs, 1)
	assert.Equal(t, "leads@example.test", msgs[0].From)
	assert.Equal(t, []string{"boss@example.test", "ops@example.test"}, msgs[0].To)
	assert.Contains(t, msgs[0].Data, `filename=leads-round-`)

	csv := decodeAttachment(t, msgs[0].Data)
	assert.Contains(t, csv, "lead_id,platform,platform_user_id")
	assert.Contains(t, csv, "telegram,1,u1")
	assert.Contains(t, csv, "any game like this one?")

	runs, err := w.svc.ListRuns(ctx, w.user, nil)
	require.NoError(t, err)
	assert.Equal(t, NotifySent, runs[0].NotifyStatus)
}

func TestRunner_Deliver_KeepsTheDeliveryWhenMailFails(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	_, err := w.svc.SaveSetting(ctx, w.user, SettingEmail, "", map[string]string{
		"host": "127.0.0.1", "port": "1", "from": "leads@example.test", "tls": smtpNoTLS,
	})
	require.NoError(t, err)
	scope := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, scope, tgGroup("705", "rogue", "Rogue"))
	_, err = w.svc.SetTarget(ctx, scope, 1)
	require.NoError(t, err)
	w.mustSaveQualified(t, scope, src, 1, 1)

	d, err := w.svc.Deliver(ctx, scope, DeliverOptions{})
	require.NoError(t, err)
	assert.Equal(t, RunStatusCompleted, d.Status)
	assert.True(t, strings.HasPrefix(d.Email, NotifyFailed+": "), d.Email)

	runs, err := w.svc.ListRuns(ctx, w.user, nil)
	require.NoError(t, err)
	assert.Equal(t, NotifyFailed, runs[0].NotifyStatus)
	assert.Contains(t, runs[0].NotifyError, "cannot reach")
}

// decodeAttachment returns the base64 body of the part carrying a filename.
func decodeAttachment(t *testing.T, data string) string {
	t.Helper()
	_, rest, ok := strings.Cut(data, "filename=")
	require.True(t, ok, "message has no attachment")
	_, body, ok := strings.Cut(rest, "\r\n\r\n")
	require.True(t, ok)
	body, _, _ = strings.Cut(body, "\r\n--")
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body, "\r\n", ""))
	require.NoError(t, err)
	return string(raw)
}

func TestRunner_ProgressSince_CountsEveryLeadOfTheOwnerOutsideATask(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	first, second := w.seedTask(t), w.seedTask(t)
	src := w.mustAddTelegramGroup(t, first, tgGroup("710", "rpg_fans", "RPG"))
	w.mustSaveQualified(t, first, src, 1, 2)
	w.mustSaveQualified(t, second, src, 3, 1)
	_, err := w.db.Exec(`UPDATE leads SET created_at = $2 WHERE user_id = $1 AND platform_user_id = '1'`,
		w.user, time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	since := time.Date(2026, 10, 17, 0, 0, 0, 0, time.FixedZone("Asia/Bangkok", 7*3600))
	chat := Scope{UserID: w.user, FlowID: first.FlowID}

	inTask, err := w.svc.ProgressSince(ctx, first, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 2, inTask.NewLeads, "a task counts its own flow")
	assert.Zero(t, inTask.Qualified)

	all, err := w.svc.ProgressSince(ctx, chat, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 3, all.Qualified, "outside a task every flow of the owner counts")
	assert.Empty(t, all.Since)

	today, err := w.svc.ProgressSince(ctx, chat, since)
	require.NoError(t, err)
	assert.Equal(t, 2, today.Qualified)
	assert.Equal(t, "2026-10-17T00:00:00+07:00", today.Since)

	leads, err := w.svc.ListLeads(ctx, w.user, LeadFilter{Since: since})
	require.NoError(t, err)
	got := []string{}
	for _, l := range leads {
		got = append(got, l.PlatformUserID)
	}
	assert.ElementsMatch(t, []string{"2", "3"}, got)
}

func TestRunner_Deliver_HasContactDeliversOnlyReachableLeads(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)
	src := w.mustAddTelegramGroup(t, scope, tgGroup("711", "mmo_trade", "MMO"))
	reachable := qualified("51", src, "m1")
	reachable.Contacts = []Contact{{Type: "line", Value: "ann_51"}}
	_, err := w.svc.UpsertLeads(ctx, scope, []LeadInput{reachable, qualified("52", src, "m2")})
	require.NoError(t, err)

	d, err := w.svc.Deliver(ctx, scope, DeliverOptions{AllowPartial: true, HasContact: true})
	require.NoError(t, err)
	require.Len(t, d.Leads, 1)
	assert.Equal(t, "51", d.Leads[0].PlatformUserID)
}
