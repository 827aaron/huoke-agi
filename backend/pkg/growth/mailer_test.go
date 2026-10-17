package growth

import (
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMailer_ParseRecipients_AcceptsCommaSemicolonAndLines(t *testing.T) {
	got, err := parseRecipients(" a@example.test; Boss <b@example.test>,\nc@example.test ")
	require.NoError(t, err)
	assert.Equal(t, []string{"a@example.test", "b@example.test", "c@example.test"}, got)

	_, err = parseRecipients("a@example.test, not-an-address")
	assert.ErrorContains(t, err, "not-an-address")
}

func TestMailer_BuildMessage_AttachesTheFileAndEncodesTheSubject(t *testing.T) {
	from := &mail.Address{Address: "leads@example.test"}
	raw, err := buildMessage(from, []string{"x@example.test"}, "游戏线索: 2 leads", "body text\n",
		[]attachment{{name: "leads.csv", contentType: "text/csv; charset=utf-8", data: []byte("a,b\n1,2\n")}})
	require.NoError(t, err)

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	require.NoError(t, err)
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	require.NoError(t, err)
	assert.Equal(t, "游戏线索: 2 leads", subject)

	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	require.NoError(t, err)
	mr := multipart.NewReader(msg.Body, params["boundary"])

	text, err := mr.NextPart()
	require.NoError(t, err)
	assert.Equal(t, "base64", text.Header.Get("Content-Transfer-Encoding"))
	assert.Equal(t, "body text\n", decodePart(t, text))

	file, err := mr.NextPart()
	require.NoError(t, err)
	assert.Equal(t, "leads.csv", file.FileName())
	assert.Equal(t, "a,b\n1,2\n", decodePart(t, file))
}

func decodePart(t *testing.T, part *multipart.Part) string {
	t.Helper()
	data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
	require.NoError(t, err)
	return string(data)
}

func TestMailer_DeliverySummary_CountsScores(t *testing.T) {
	subject, body := deliverySummary("Game X", []LeadView{{Score: 9}, {Score: 7}, {Score: 9}}, 50, time.Time{}, "")
	assert.Equal(t, "Game X: 3 leads delivered", subject)
	assert.Contains(t, body, "Leads delivered: 3 (target 50)")
	assert.Contains(t, body, "  score 9: 2\n  score 7: 1\n")
	assert.NotContains(t, body, "end time")
}

func TestMailer_DeliverySummary_SaysTheRoundClosedAtTheEndTime(t *testing.T) {
	deadline := time.Date(2026, 10, 19, 6, 0, 0, 0, time.FixedZone("ICT", 7*3600))
	_, body := deliverySummary("Game X", []LeadView{{Score: 9}}, 50, deadline, "English")
	assert.Contains(t, body, "Delivered at the schedule's end time, 2026-10-18 23:00 UTC: 1 of the target 50.\n")
}

func TestMailer_DeliverySummary_WritesInTheCustomersLanguage(t *testing.T) {
	deadline := time.Date(2026, 10, 18, 23, 0, 0, 0, time.UTC)
	subject, body := deliverySummary("", []LeadView{{Score: 9}, {Score: 9}}, 50, deadline, "Chinese")
	assert.Equal(t, "获客任务：已交付 2 条线索", subject)
	assert.Equal(t, "业务：获客任务\n已交付线索：2 条（目标 50 条）\n"+
		"已按定时结束时间 2026-10-18 23:00（UTC）交付：2 条，目标 50 条。\n\n"+
		"分数分布：\n  9 分：2 条\n\n完整名单和证据见附件 CSV。\n", body)
}
