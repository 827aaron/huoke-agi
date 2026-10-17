package growth

import (
	"bytes"
	"encoding/csv"
	"pentagi/pkg/templates"
	"strconv"
	"strings"
	"time"
)

var leadsCSVHeader = []string{
	"lead_id", "platform", "platform_user_id", "username", "display_name", "profile_link",
	"score", "tier", "match_reason", "evidence_excerpt", "evidence_link", "evidence_time",
	"evidence_count", "delivered_at", "contacts",
}

// LeadsCSV renders leads one row each, with the first piece of evidence inline, and the
// text it writes itself in lang. It starts with a UTF-8 byte order mark because Excel
// otherwise opens Chinese text as mojibake.
func LeadsCSV(leads []LeadView, lang string) []byte {
	platformOnly := templates.NoticeText(templates.NoticeCSVPlatformOnly, lang)
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	_ = w.Write(leadsCSVHeader)
	for _, l := range leads {
		row := []string{
			strconv.FormatInt(l.ID, 10), l.Platform, l.PlatformUserID, l.Username, l.DisplayName,
			profileLink(l), strconv.Itoa(l.Score), l.Tier, l.MatchReason, "", "", "",
			strconv.Itoa(len(l.Evidence)), formatTime(l.DeliveredAt), contactsCell(l.Contacts, platformOnly),
		}
		if len(l.Evidence) > 0 {
			e := l.Evidence[0]
			row[9], row[10] = e.Excerpt, e.MessageLink
			if e.MessageTime != nil {
				row[11] = formatTime(e.MessageTime)
			} else {
				row[11] = formatTime(&e.CapturedAt)
			}
		}
		_ = w.Write(row)
	}
	w.Flush()
	return buf.Bytes()
}

// contactsCell gives platformOnly for a lead reachable through its platform account alone.
func contactsCell(contacts []Contact, platformOnly string) string {
	if len(contacts) == 0 {
		return platformOnly
	}
	parts := make([]string, 0, len(contacts))
	for _, c := range contacts {
		parts = append(parts, c.Type+": "+c.Value)
	}
	return strings.Join(parts, "; ")
}

func profileLink(l LeadView) string {
	switch {
	case l.IdentityKind == "profile_url":
		if strings.Contains(l.PlatformUserID, "://") {
			return l.PlatformUserID
		}
		return "https://" + l.PlatformUserID
	case l.Platform == "telegram" && l.Username != "":
		return "https://t.me/" + strings.TrimPrefix(l.Username, "@")
	default:
		return ""
	}
}

func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
