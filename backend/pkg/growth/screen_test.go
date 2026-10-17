package growth

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"pentagi/pkg/growth/screening"
)

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
