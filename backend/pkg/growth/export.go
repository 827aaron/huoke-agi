package growth

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/templates"

	"github.com/sirupsen/logrus"
)

//go:embed leadscsv.json
var leadsCSVSpecJSON []byte

// leadsCSVSpec is the vocabulary of the sheet: the column order and every label it writes,
// per language key of templates.LanguageKey. The web app's own export writes the same
// sheet from its copy; its test reads this file to keep the two equal.
type leadsCSVSpec struct {
	Columns          []string                     `json:"columns"`
	Header           map[string]map[string]string `json:"header"`
	Tier             map[string]map[string]string `json:"tier"`
	Signal           map[string]map[string]string `json:"signal"`
	Platform         map[string]map[string]string `json:"platform"`
	Contact          map[string]map[string]string `json:"contact"`
	File             map[string]map[string]string `json:"file"`
	ContactSeparator map[string]string            `json:"contactSeparator"`
}

var leadsCSV = mustLeadsCSVSpec()

func mustLeadsCSVSpec() leadsCSVSpec {
	var spec leadsCSVSpec
	if err := json.Unmarshal(leadsCSVSpecJSON, &spec); err != nil {
		panic("leadscsv.json: " + err.Error())
	}
	return spec
}

// LeadsCSVContext is what the sheet needs beside the leads. SignalNames names the
// signals the built-in list does not, by profile dimension key; Sources gives the title
// and link of each source id; a nil Location writes times in UTC.
type LeadsCSVContext struct {
	Lang        string
	Location    *time.Location
	SignalNames map[string]string
	Sources     map[int64]SourceRef
}

type SourceRef struct {
	Title string
	Link  string
}

func (c LeadsCSVContext) location() *time.Location {
	if c.Location == nil {
		return time.UTC
	}
	return c.Location
}

// text is the label of key in the context's language, English when the language has
// none, and the key itself when the table does not know it.
func (c LeadsCSVContext) text(table map[string]map[string]string, key string) string {
	texts, ok := table[key]
	if !ok {
		return key
	}
	if t, ok := texts[templates.LanguageKey(c.Lang)]; ok {
		return t
	}
	return texts["english"]
}

func (c LeadsCSVContext) fileText(key string) string { return c.text(leadsCSV.File, key) }

// LeadsCSV writes one row per lead: who they are and how to reach them, the score, the
// signals and the agent's reason, and the newest piece of evidence as the provenance. It
// starts with a UTF-8 byte order mark because Excel otherwise opens Chinese text as mojibake.
func LeadsCSV(leads []LeadView, c LeadsCSVContext) []byte {
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	w.UseCRLF = true

	header := make([]string, len(leadsCSV.Columns))
	for i, col := range leadsCSV.Columns {
		header[i] = c.text(leadsCSV.Header, col)
	}
	_ = w.Write(header)

	for _, l := range leads {
		cells := leadsCSVCells(l, c)
		row := make([]string, len(leadsCSV.Columns))
		for i, col := range leadsCSV.Columns {
			row[i] = cells[col]
		}
		_ = w.Write(row)
	}
	w.Flush()
	return buf.Bytes()
}

func leadsCSVCells(l LeadView, c LeadsCSVContext) map[string]string {
	cells := map[string]string{
		"name":     firstNonEmpty(l.DisplayName, l.Username, l.PlatformUserID),
		"username": l.PlatformUserID,
		"platform": c.text(leadsCSV.Platform, l.Platform),
		"contacts": firstNonEmpty(c.contactsCell(l.Contacts), profileLink(l)),
		"score":    fmt.Sprintf("%d · %s", l.Score, c.text(leadsCSV.Tier, l.Tier)),
		"signals":  c.signalsCell(l.Signals),
		"reason":   l.MatchReason,
	}
	if l.Username != "" {
		cells["username"] = "@" + strings.TrimPrefix(l.Username, "@")
	}
	if e := newestEvidence(l.Evidence); e != nil {
		src := c.Sources[e.SourceID]
		cells["source"] = src.Title
		cells["quote"] = e.Excerpt
		cells["quoteUrl"] = firstNonEmpty(e.MessageLink, src.Link)
		cells["quoteTime"] = postedAt(*e).In(c.location()).Format("2006-01-02 15:04")
	}
	return cells
}

func (c LeadsCSVContext) contactsCell(contacts []Contact) string {
	sep := c.ContactSeparator()
	parts := make([]string, 0, len(contacts))
	for _, ct := range contacts {
		parts = append(parts, c.text(leadsCSV.Contact, ct.Type)+sep+ct.Value)
	}
	return strings.Join(parts, "; ")
}

// ContactSeparator sits between a contact's type and its value, a full-width colon in Chinese.
func (c LeadsCSVContext) ContactSeparator() string {
	if sep, ok := leadsCSV.ContactSeparator[templates.LanguageKey(c.Lang)]; ok {
		return sep
	}
	return leadsCSV.ContactSeparator["english"]
}

func (c LeadsCSVContext) signalsCell(signals []string) string {
	labels := make([]string, 0, len(signals))
	for _, key := range signals {
		switch {
		case leadsCSV.Signal[key] != nil:
			labels = append(labels, c.text(leadsCSV.Signal, key))
		case c.SignalNames[key] != "":
			labels = append(labels, c.SignalNames[key])
		default:
			labels = append(labels, key)
		}
	}
	return strings.Join(labels, "、")
}

func postedAt(e EvidenceView) time.Time {
	if e.MessageTime != nil && !e.MessageTime.IsZero() {
		return *e.MessageTime
	}
	return e.CapturedAt
}

func newestEvidence(evidence []EvidenceView) *EvidenceView {
	var newest *EvidenceView
	for i := range evidence {
		if newest == nil || postedAt(evidence[i]).After(postedAt(*newest)) {
			newest = &evidence[i]
		}
	}
	return newest
}

// profileLink is the way to reach a lead that has no contact of its own: its Telegram
// handle, or the profile link a web lead is identified by.
func profileLink(l LeadView) string {
	switch {
	case l.Platform == "telegram" && l.Username != "":
		return "https://t.me/" + strings.TrimPrefix(l.Username, "@")
	case strings.Contains(l.PlatformUserID, "://"):
		return l.PlatformUserID
	case l.IdentityKind == "profile_url" || (l.Platform == "web" && strings.Contains(l.PlatformUserID, "/")):
		return "https://" + l.PlatformUserID
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

var fileUnsafe = regexp.MustCompile(`[\\/:*?"<>|\s]+`)

// LeadsCSVFileName names a sheet after the business, what was exported and when:
// `潜客_<business>_<scope>_<yyyymmdd-hhmm>.csv`, the way the web app names its download.
func LeadsCSVFileName(business, scope string, c LeadsCSVContext, now time.Time) string {
	if strings.TrimSpace(business) == "" {
		business = c.fileText("unnamed")
	}
	return fmt.Sprintf("%s_%s_%s_%s.csv",
		c.fileText("prefix"),
		fileUnsafe.ReplaceAllString(strings.TrimSpace(business), "_"),
		fileUnsafe.ReplaceAllString(strings.TrimSpace(scope), "_"),
		now.In(c.location()).Format("20060102-1504"),
	)
}

// RoundScope is the scope of a sheet that holds one round: `第N轮`.
func (c LeadsCSVContext) RoundScope(round int) string {
	return fmt.Sprintf(c.fileText("round"), round)
}

// FilteredScope is the scope of a sheet that holds whatever a filter selected.
func (c LeadsCSVContext) FilteredScope() string { return c.fileText("filtered") }

// DeliveredScope is the scope of a sheet that holds a flow's delivered leads.
func (c LeadsCSVContext) DeliveredScope() string { return c.fileText("delivered") }

// LeadsCSVContext gathers what a customer's sheet needs: the language of the flow, the
// names of their profiles' own scoring dimensions, their sources, and the zone of the
// flow's schedule, else of any schedule they have, else UTC. A lookup that fails leaves
// its part empty rather than failing the export; the sheet still reads.
func (s *Service) LeadsCSVContext(ctx context.Context, userID, flowID int64) LeadsCSVContext {
	logger := logrus.WithContext(ctx).WithField("user_id", userID)
	c := LeadsCSVContext{
		Lang:        s.Language(ctx, userID, flowID),
		SignalNames: map[string]string{},
		Sources:     map[int64]SourceRef{},
	}

	profiles, err := s.ListProfiles(ctx, userID)
	if err != nil {
		logger.WithError(err).Warn("leads sheet goes out without the profiles' dimension names")
	}
	for _, p := range profiles {
		if p.Scoring == nil {
			continue
		}
		for _, d := range p.Scoring.Dimensions {
			if _, builtin := leadsCSV.Signal[d.Key]; !builtin && d.Name != "" {
				c.SignalNames[d.Key] = d.Name
			}
		}
	}

	sources, err := s.db.GetUserGrowthSources(ctx, userID)
	if err != nil {
		logger.WithError(err).Warn("leads sheet goes out without source titles")
	}
	for _, src := range sources {
		c.Sources[src.ID] = SourceRef{Title: src.Title, Link: src.Link}
	}

	c.Location = s.scheduleLocation(ctx, userID, flowID)
	return c
}

func (s *Service) scheduleLocation(ctx context.Context, userID, flowID int64) *time.Location {
	params := database.GetUserGrowthSchedulesParams{UserID: userID}
	if flowID != 0 {
		params.FlowID = sql.NullInt64{Int64: flowID, Valid: true}
	}
	for _, p := range []database.GetUserGrowthSchedulesParams{params, {UserID: userID}} {
		rows, err := s.db.GetUserGrowthSchedules(ctx, p)
		if err != nil {
			return time.UTC
		}
		for _, row := range rows {
			if loc, err := time.LoadLocation(row.Timezone); err == nil && row.Timezone != "" {
				return loc
			}
		}
		if !p.FlowID.Valid {
			break
		}
	}
	return time.UTC
}
