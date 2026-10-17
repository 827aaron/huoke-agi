package templates

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// Notice is a user-visible line the backend writes itself, where no model is at hand to
// write it in the conversation's language. Every language's text takes the same verbs in
// the same order: one %s, except deadline_delivered's time, delivered and target.
type Notice string

const (
	NoticeAssistantFailed   Notice = "assistant_failed"
	NoticeToolFailed        Notice = "tool_failed"
	NoticeToolStopped       Notice = "tool_stopped"
	NoticeDeadlineDelivered Notice = "deadline_delivered"
)

//go:embed notices/notices.json
var noticesJSON []byte

var notices = mustNotices()

// languageAliases maps other names the language chooser gives to a key of notices.json.
var languageAliases = map[string]string{
	"mandarin":            "chinese",
	"simplified chinese":  "chinese",
	"chinese simplified":  "chinese",
	"chinese traditional": "traditional chinese",
}

func mustNotices() map[Notice]map[string]string {
	var out map[Notice]map[string]string
	if err := json.Unmarshal(noticesJSON, &out); err != nil {
		panic("notices.json: " + err.Error())
	}
	return out
}

// NoticeText is the notice in lang, the English language name the language chooser
// returns, falling back to English for a language without a translation.
func NoticeText(n Notice, lang string) string {
	texts := notices[n]
	key := strings.ToLower(strings.TrimSpace(lang))
	if alias, ok := languageAliases[key]; ok {
		key = alias
	}
	if text, ok := texts[key]; ok {
		return text
	}
	return texts["english"]
}
