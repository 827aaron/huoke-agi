//go:build postgres

package growth

import (
	"context"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMailer_EmailLeads_MailsTheRoundOrItsQualifiedLeads(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	scope := w.seedTask(t)

	_, err := w.svc.EmailLeads(ctx, w.user, scope.FlowID, scope.TaskID)
	assert.ErrorIs(t, err, ErrNoSMTP)

	sink := startSMTPSink(t)
	w.saveSink(t, sink, "boss@example.test")
	_, err = w.svc.EmailLeads(ctx, w.user, scope.FlowID, scope.TaskID)
	assert.ErrorIs(t, err, ErrNoLeadsToSend)

	src := w.mustAddTelegramGroup(t, scope, tgGroup("705", "moba", "Moba"))
	w.mustSaveQualified(t, scope, src, 1, 2)
	to, err := w.svc.EmailLeads(ctx, w.user, scope.FlowID, scope.TaskID)
	require.NoError(t, err)
	assert.Equal(t, []string{"boss@example.test"}, to)
	msgs := sink.Messages()
	require.Len(t, msgs, 1)
	assert.Contains(t, decodeAttachment(t, msgs[0].Data), "telegram,2,u2")

	_, err = w.svc.SetTarget(ctx, scope, 1)
	require.NoError(t, err)
	_, err = w.svc.Deliver(ctx, scope, DeliverOptions{})
	require.NoError(t, err)
	_, err = w.svc.EmailLeads(ctx, w.user, scope.FlowID, scope.TaskID)
	require.NoError(t, err)
	msgs = sink.Messages()
	require.Len(t, msgs, 3, "the delivery mail and the one asked for")
	assert.Contains(t, msgs[2].Data, "filename=leads-round-")
	rows, err := csv.NewReader(strings.NewReader(decodeAttachment(t, msgs[2].Data))).ReadAll()
	require.NoError(t, err)
	assert.Len(t, rows, 2, "the header and the round's one delivered lead")
}
