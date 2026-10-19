package templates

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// Notice is a user-visible line the backend writes itself, where no model is at hand to
// write it in the conversation's language. Every language's text of a notice takes the
// same verbs in the same order.
type Notice string

const (
	NoticeAssistantFailed   Notice = "assistant_failed"
	NoticeToolFailed        Notice = "tool_failed"
	NoticeToolStopped       Notice = "tool_stopped"
	NoticeDeadlineDelivered Notice = "deadline_delivered"

	NoticeMailSubject         Notice = "mail_subject"
	NoticeMailDefaultBusiness Notice = "mail_default_business"
	NoticeMailBusiness        Notice = "mail_business"
	NoticeMailDelivered       Notice = "mail_delivered"
	NoticeMailAtDeadline      Notice = "mail_at_deadline"
	NoticeMailScoreHeader     Notice = "mail_score_header"
	NoticeMailScoreLine       Notice = "mail_score_line"
	NoticeMailAttached        Notice = "mail_attached"
	NoticeMailTestSubject     Notice = "mail_test_subject"
	NoticeMailTestBody        Notice = "mail_test_body"
)

// Notices lists every notice, for the check that each has every language.
var Notices = []Notice{
	NoticeAssistantFailed, NoticeToolFailed, NoticeToolStopped, NoticeDeadlineDelivered,
	NoticeMailSubject, NoticeMailDefaultBusiness, NoticeMailBusiness, NoticeMailDelivered, NoticeMailAtDeadline,
	NoticeMailScoreHeader, NoticeMailScoreLine, NoticeMailAttached, NoticeMailTestSubject, NoticeMailTestBody,
}

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
	if text, ok := texts[LanguageKey(lang)]; ok {
		return text
	}
	return texts["english"]
}

// LanguageKey is the translation-table key of a language name the language chooser
// returns: lower-cased, with the names it also gives mapped to the key the tables use.
func LanguageKey(lang string) string {
	key := strings.ToLower(strings.TrimSpace(lang))
	if alias, ok := languageAliases[key]; ok {
		return alias
	}
	return key
}
