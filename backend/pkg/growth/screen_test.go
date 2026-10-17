package growth

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"pentagi/pkg/growth/screening"
)

func TestScreen_ParseScreening_ReadsTheThreshold(t *testing.T) {
	got, err := parseScreening(map[string]string{})
	assert.NoError(t, err)
	assert.Equal(t, 0.6, got)
	got, err = parseScreening(map[string]string{"threshold": " 0.45 "})
	assert.NoError(t, err)
	assert.Equal(t, 0.45, got)
	for _, bad := range []string{"0", "1", "high", "-0.2"} {
		_, err := parseScreening(map[string]string{"threshold": bad})
		assert.ErrorIs(t, err, ErrInvalidSetting, bad)
	}
}

func TestScreen_ScreenMessages_NamesEachSender(t *testing.T) {
	got := screenMessages([]MessageView{
		{MessageID: "3", SenderID: "11", SenderUsername: "ann", SenderName: "Ann", Text: "looking\nfor a guild"},
		{MessageID: "2", SenderID: "12", SenderIsBot: true, Text: "join now"},
	})
	assert.Equal(t, []screening.Message{
		{ID: "3", Sender: "@ann Ann", Text: "looking for a guild"},
		{ID: "2", Sender: "12 (bot)", Text: "join now"},
	}, got)
}
