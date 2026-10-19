package templates

import (
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var formatVerb = regexp.MustCompile(`%[a-z]`)

func TestNotices_NoticeText_EveryNoticeHasEveryLanguageWithTheSameVerbs(t *testing.T) {
	require.Len(t, notices, len(Notices), "notices.json and Notices list the same notices")
	for _, n := range Notices {
		texts, ok := notices[n]
		require.True(t, ok, n)
		english := formatVerb.FindAllString(texts["english"], -1)
		for _, lang := range []string{"english", "chinese", "traditional chinese"} {
			require.NotEmpty(t, texts[lang], "%s in %s", n, lang)
			assert.True(t, slices.Equal(english, formatVerb.FindAllString(texts[lang], -1)), "%s in %s", n, lang)
		}
	}
}

func TestNotices_NoticeText_FallsBackToEnglishAndReadsAliases(t *testing.T) {
	assert.Equal(t, "线索交付邮件测试", NoticeText(NoticeMailTestSubject, " Simplified Chinese "))
	assert.Equal(t, "線索交付郵件測試", NoticeText(NoticeMailTestSubject, "Chinese Traditional"))
	assert.Equal(t, "Lead delivery email test", NoticeText(NoticeMailTestSubject, "Thai"))
	assert.Equal(t, "Lead delivery email test", NoticeText(NoticeMailTestSubject, ""))
	assert.Equal(t, "chinese", LanguageKey("Mandarin"))
	assert.Equal(t, "english", LanguageKey(" English "))
}
