package growth

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"pentagi/pkg/growth/screening"
)

// The three prompts a profile is. Need is who to look for, Exclude who to score lower,
// ScoringText the Jev rules. Sections are extra prompts the owner added, in order.
const (
	maxPromptRunes  = 8000
	maxSections     = 20
	maxSectionTitle = 80
	editorUser      = "user"
	editorAssistant = "assistant"
)

// ProfileSection is one prompt the owner added after the three fixed ones.
type ProfileSection struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// ProfileRevision is the profile as it was before the last assistant edit.
type ProfileRevision struct {
	At       time.Time        `json:"at"`
	Title    string           `json:"title"`
	Name     string           `json:"name"`
	Need     string           `json:"need"`
	Exclude  string           `json:"exclude"`
	Scoring  string           `json:"scoring"`
	Sections []ProfileSection `json:"sections,omitempty"`
}

// scoringText is the Jev rules. Stored and sent as text; a saved profile may still
// carry the older scoring object, which is formatted into the same text on read.
type scoringText string

func (t *scoringText) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*t = scoringText(s)
		return nil
	}
	var sc Scoring
	if err := json.Unmarshal(data, &sc); err != nil {
		return err
	}
	sc.normalize()
	*t = scoringText(FormatScoring(sc, 0))
	return nil
}

func (t scoringText) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(t))
}

// ProfileOp is one edit of a profile's prompts. Action is set, append, add_section or
// remove_section. Field is need, exclude or scoring; a section is chosen by SectionID or
// SectionTitle. NewTitle renames a section. ProfileTitle renames the profile.
type ProfileOp struct {
	Action       string `json:"action"`
	Field        string `json:"field,omitempty"`
	SectionID    string `json:"section_id,omitempty"`
	SectionTitle string `json:"section_title,omitempty"`
	NewTitle     string `json:"new_title,omitempty"`
	Text         string `json:"text,omitempty"`
	ProfileTitle string `json:"profile_title,omitempty"`
}

// DefaultScoringText is the Jev rules a profile uses when it sets none.
func DefaultScoringText() string {
	return FormatScoring(DefaultScoring(), DefaultUnwantedPenalty)
}

// FormatScoring is the rules as the profile stores them. A dimension is
// "key = points | name | description"; recent, qualified, watchlist and unwanted are
// their own lines. ParseScoring reads it back.
func FormatScoring(sc Scoring, penalty int) string {
	var b strings.Builder
	for _, d := range sc.Dimensions {
		fmt.Fprintf(&b, "%s = %d | %s | %s\n", d.Key, d.Points, d.Name, d.Description)
	}
	if sc.Recent != nil && sc.Recent.Points > 0 {
		fmt.Fprintf(&b, "recent = %d\n", sc.Recent.Points)
	}
	fmt.Fprintf(&b, "qualified >= %d\nwatchlist >= %d\n", sc.Qualified, sc.Watchlist)
	if penalty > 0 {
		fmt.Fprintf(&b, "unwanted = -%d\n", penalty)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ParseScoring reads FormatScoring and the shorter lines an owner types:
// "name = points", "name = points | description", "qualified >= N", "watchlist >= N",
// "recent = N", "unwanted = -N". The penalty is 0 when the line is absent.
func ParseScoring(text string) (Scoring, int, error) {
	sc := Scoring{}
	penalty := 0
	var dims []ScoringDimension
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if label, value, isThreshold := strings.Cut(line, ">="); isThreshold {
			n, err := threshold(value, i+1, strings.TrimSpace(label))
			if err != nil {
				return Scoring{}, 0, err
			}
			switch strings.ToLower(strings.TrimSpace(label)) {
			case "qualified":
				sc.Qualified = n
			case "watchlist":
				sc.Watchlist = n
			default:
				return Scoring{}, 0, fmt.Errorf("%w: line %d: %q is not a scoring line", ErrInvalidProfile, i+1, line)
			}
			continue
		}
		left, right, ok := strings.Cut(line, "=")
		if !ok {
			return Scoring{}, 0, fmt.Errorf("%w: line %d: %q is not a scoring line", ErrInvalidProfile, i+1, line)
		}
		left, right = strings.TrimSpace(left), strings.TrimSpace(right)
		switch strings.ToLower(left) {
		case "qualified":
			n, err := threshold(right, i+1, "qualified")
			if err != nil {
				return Scoring{}, 0, err
			}
			sc.Qualified = n
		case "watchlist":
			n, err := threshold(right, i+1, "watchlist")
			if err != nil {
				return Scoring{}, 0, err
			}
			sc.Watchlist = n
		case "recent":
			n, err := points(right, i+1, "recent")
			if err != nil {
				return Scoring{}, 0, err
			}
			sc.Recent = &RecentDimension{Points: n}
		case "unwanted":
			n, err := signedPoints(right, i+1)
			if err != nil {
				return Scoring{}, 0, err
			}
			if n < 0 {
				n = -n
			}
			penalty = n
		default:
			dim, err := dimensionLine(left, right, i+1)
			if err != nil {
				return Scoring{}, 0, err
			}
			dims = append(dims, dim)
		}
	}
	sc.Dimensions = dims
	sc.normalize()
	for i, d := range sc.Dimensions {
		if d.Description != "" {
			continue
		}
		for _, sig := range screening.Signals {
			if sig.Key == d.Key {
				sc.Dimensions[i].Name = sig.Name
				sc.Dimensions[i].Description = sig.Description
			}
		}
	}
	if err := validateScoring(sc, penalty); err != nil {
		return Scoring{}, 0, err
	}
	return sc, penalty, nil
}

func threshold(right string, line int, label string) (int, error) {
	right = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(right), ">="))
	n, err := strconv.Atoi(right)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%w: line %d: %s needs a whole number", ErrInvalidProfile, line, label)
	}
	return n, nil
}

func points(right string, line int, label string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil || n < 0 || n > maxDimensionPoints {
		return 0, fmt.Errorf("%w: line %d: %s points are between 0 and %d", ErrInvalidProfile, line, label, maxDimensionPoints)
	}
	return n, nil
}

func signedPoints(right string, line int) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil || n < -maxDimensionPoints || n > maxDimensionPoints {
		return 0, fmt.Errorf("%w: line %d: unwanted points are between -%d and %d", ErrInvalidProfile, line, maxDimensionPoints, maxDimensionPoints)
	}
	return n, nil
}

func dimensionLine(left, right string, line int) (ScoringDimension, error) {
	head, rest, hasBar := strings.Cut(right, "|")
	n, err := points(head, line, strconv.Quote(left))
	if err != nil {
		return ScoringDimension{}, err
	}
	dim := ScoringDimension{Name: left, Points: n}
	if !hasBar {
		return dim, nil
	}
	name, desc, two := strings.Cut(rest, "|")
	if two {
		dim.Key = left
		dim.Name = strings.TrimSpace(name)
		dim.Description = strings.TrimSpace(desc)
		if dim.Name == "" {
			return ScoringDimension{}, fmt.Errorf("%w: every scoring dimension needs a name", ErrInvalidProfile)
		}
		return dim, nil
	}
	dim.Description = strings.TrimSpace(name)
	return dim, nil
}

func validateScoring(sc Scoring, penalty int) error {
	if len(sc.Dimensions) == 0 {
		return fmt.Errorf("%w: scoring needs at least one dimension", ErrInvalidProfile)
	}
	for _, d := range sc.Dimensions {
		if d.Name == "" {
			return fmt.Errorf("%w: every scoring dimension needs a name", ErrInvalidProfile)
		}
		if d.Points < 0 || d.Points > maxDimensionPoints {
			return fmt.Errorf("%w: signal %q: points are between 0 and %d", ErrInvalidProfile, d.Name, maxDimensionPoints)
		}
	}
	if sc.Recent != nil && (sc.Recent.Points < 0 || sc.Recent.Points > maxDimensionPoints) {
		return fmt.Errorf("%w: recent points are between 0 and %d", ErrInvalidProfile, maxDimensionPoints)
	}
	if penalty < 0 || penalty > maxDimensionPoints {
		return fmt.Errorf("%w: unwanted points are between 0 and %d", ErrInvalidProfile, maxDimensionPoints)
	}
	if sc.Watchlist < 1 || sc.Qualified <= sc.Watchlist {
		return fmt.Errorf("%w: scoring needs 1 <= watchlist < qualified", ErrInvalidProfile)
	}
	if sc.maxScore() < sc.Qualified {
		return fmt.Errorf("%w: the points add up to less than qualified, so no lead could qualify", ErrInvalidProfile)
	}
	return nil
}

func renderNeed(p Profile) string {
	var lines []string
	add := func(label, value string) {
		if value = strings.TrimSpace(value); value != "" {
			lines = append(lines, label+": "+value)
		}
	}
	add("Business", p.Name)
	add("Offer", strings.Join(strings.Fields(p.Pitch), " "))
	add("Products", strings.Join(p.Games, ", "))
	add("Category", p.Genre)
	add("Regions", strings.Join(p.Regions, ", "))
	add("Languages", strings.Join(p.Languages, ", "))
	if p.DefaultTarget > 0 {
		add("Lead target", strconv.Itoa(p.DefaultTarget))
	}
	add("Signal words", strings.Join(p.Signals, "; "))
	if len(p.ContactTypes) > 0 && !slicesEqual(p.ContactTypes, []string{ContactPlatformHandle}) {
		add("Contacts to find", strings.Join(p.ContactTypes, ", "))
	}
	add("Notes", strings.Join(strings.Fields(p.Notes), " "))
	return strings.Join(lines, "\n")
}

func renderExclude(p Profile) string {
	var lines []string
	for _, c := range p.Competitors {
		lines = append(lines, "Competitor: "+c)
	}
	lines = append(lines, p.Unwanted...)
	return strings.Join(lines, "\n")
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// foldTexts fills the three prompts from the structured fields when those prompts are
// empty, then reads the prompts back into the structured fields the rest of the
// package judges with.
func (p *Profile) foldTexts() error {
	if p.UnwantedPenalty < 0 {
		return fmt.Errorf("%w: unwanted_penalty cannot be negative", ErrInvalidProfile)
	}
	if p.Scoring != nil {
		sc := *p.Scoring
		sc.normalize()
		p.Scoring = &sc
	}
	// Genre alone is not a profile: the category is rendered into Need only when something
	// else already says who the business is or who to look for.
	legacyBody := p.Name != "" || p.Pitch != "" || len(p.Games) > 0 || len(p.Regions) > 0 ||
		len(p.Languages) > 0 || len(p.Signals) > 0 || len(p.Competitors) > 0 || len(p.Unwanted) > 0 ||
		p.Notes != "" || p.DefaultTarget > 0 || p.Scoring != nil
	if strings.TrimSpace(p.Need) == "" && legacyBody {
		p.Need = renderNeed(*p)
	}
	if strings.TrimSpace(p.Exclude) == "" && (len(p.Competitors) > 0 || len(p.Unwanted) > 0) {
		p.Exclude = renderExclude(*p)
	}
	if strings.TrimSpace(string(p.ScoringText)) == "" && (p.Scoring != nil || strings.TrimSpace(p.Need) != "" || strings.TrimSpace(p.Exclude) != "") {
		sc := DefaultScoring()
		if p.Scoring != nil {
			sc = *p.Scoring
		}
		pen := p.UnwantedPenalty
		if pen <= 0 {
			pen = DefaultUnwantedPenalty
		}
		p.ScoringText = scoringText(FormatScoring(sc, pen))
	}
	p.Need = strings.TrimSpace(p.Need)
	p.Exclude = strings.TrimSpace(p.Exclude)
	p.normalizeSections()
	if err := p.limits(); err != nil {
		return err
	}
	if strings.TrimSpace(string(p.ScoringText)) != "" {
		sc, pen, err := ParseScoring(string(p.ScoringText))
		if err != nil {
			return err
		}
		p.Scoring = &sc
		if pen > 0 {
			p.UnwantedPenalty = pen
		}
	}
	if p.Exclude != "" {
		p.Competitors, p.Unwanted = parseExclude(p.Exclude)
	}
	parseNeed(p)
	return nil
}

func (p *Profile) limits() error {
	for _, text := range []string{p.Need, p.Exclude, string(p.ScoringText)} {
		if utf8.RuneCountInString(text) > maxPromptRunes {
			return fmt.Errorf("%w: a prompt is longer than %d characters", ErrInvalidProfile, maxPromptRunes)
		}
	}
	if len(p.Sections) > maxSections {
		return fmt.Errorf("%w: at most %d added prompts", ErrInvalidProfile, maxSections)
	}
	return nil
}

func (p *Profile) normalizeSections() {
	out := make([]ProfileSection, 0, len(p.Sections))
	seen := map[string]bool{}
	for _, s := range p.Sections {
		s.ID, s.Title, s.Body = strings.TrimSpace(s.ID), strings.TrimSpace(s.Title), strings.TrimSpace(s.Body)
		if s.Title == "" && s.Body == "" {
			continue
		}
		if utf8.RuneCountInString(s.Title) > maxSectionTitle {
			s.Title = string([]rune(s.Title)[:maxSectionTitle])
		}
		if s.ID == "" || seen[s.ID] {
			for n := len(out) + 1; ; n++ {
				if s.ID = fmt.Sprintf("s%d", n); !seen[s.ID] {
					break
				}
			}
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	p.Sections = out
}

func parseExclude(text string) (competitors, unwanted []string) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "Competitor:"); ok {
			if c := strings.TrimSpace(rest); c != "" {
				competitors = append(competitors, c)
			}
			continue
		}
		unwanted = append(unwanted, line)
	}
	return competitors, unwanted
}

func parseNeed(p *Profile) {
	var notes []string
	inNotes := false
	for _, line := range strings.Split(p.Need, "\n") {
		if inNotes {
			notes = append(notes, strings.TrimSpace(line))
			continue
		}
		label, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		label, value = strings.TrimSpace(label), strings.TrimSpace(value)
		switch label {
		case "Business":
			if p.Name == "" {
				p.Name = value
			}
		case "Offer":
			p.Pitch = value
		case "Products":
			p.Games = splitComma(value)
		case "Category":
			p.Genre = value
		case "Regions":
			p.Regions = splitComma(value)
		case "Languages":
			p.Languages = splitComma(value)
		case "Lead target":
			n, _ := strconv.Atoi(value)
			p.DefaultTarget = n
		case "Signal words":
			p.Signals = splitSemi(value)
		case "Contacts to find":
			p.ContactTypes = nil
			for _, t := range splitComma(value) {
				p.ContactTypes = append(p.ContactTypes, t)
			}
		case "Notes":
			inNotes = true
			if value != "" {
				notes = append(notes, value)
			}
		}
	}
	if len(notes) > 0 {
		p.Notes = strings.TrimSpace(strings.Join(notes, "\n"))
	}
}

func splitComma(s string) []string { return trimItems(strings.Split(s, ",")) }

func splitSemi(s string) []string { return trimItems(strings.Split(s, ";")) }

// excludeLines is what Jev is asked about: every non-empty line of the exclude prompt,
// including competitor lines. A hit lowers the score; it does not drop the message.
func excludeLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func (p Profile) UnwantedLines() []string {
	if strings.TrimSpace(p.Exclude) != "" {
		return excludeLines(p.Exclude)
	}
	return p.Unwanted
}

// apply changes one prompt. The caller validates afterwards.
func (p *Profile) apply(op ProfileOp) error {
	switch op.Action {
	case "set", "append":
		return p.applyText(op)
	case "add_section":
		title := strings.TrimSpace(cmp.Or(op.NewTitle, op.SectionTitle))
		if title == "" {
			return fmt.Errorf("%w: a new prompt needs a title", ErrInvalidProfile)
		}
		p.Sections = append(p.Sections, ProfileSection{Title: title, Body: op.Text})
		p.normalizeSections()
		return nil
	case "remove_section":
		i, err := p.findSection(op)
		if err != nil {
			return err
		}
		p.Sections = append(p.Sections[:i], p.Sections[i+1:]...)
		return nil
	default:
		return fmt.Errorf("%w: unknown profile action %q", ErrInvalidProfile, op.Action)
	}
}

func (p *Profile) applyText(op ProfileOp) error {
	if op.SectionID != "" || (op.SectionTitle != "" && op.Field == "") || op.NewTitle != "" && op.Field == "" {
		i, err := p.findSection(op)
		if err != nil {
			return err
		}
		if t := strings.TrimSpace(op.NewTitle); t != "" {
			p.Sections[i].Title = t
		}
		switch op.Action {
		case "set":
			if op.Text != "" || op.NewTitle == "" {
				p.Sections[i].Body = op.Text
			}
		case "append":
			p.Sections[i].Body = appendText(p.Sections[i].Body, op.Text)
		}
		return nil
	}
	write := func(cur string) string {
		if op.Action == "append" {
			return appendText(cur, op.Text)
		}
		return op.Text
	}
	switch op.Field {
	case "need":
		p.Need = write(p.Need)
	case "exclude":
		p.Exclude = write(p.Exclude)
	case "scoring":
		p.ScoringText = scoringText(write(string(p.ScoringText)))
	case "":
		return fmt.Errorf("%w: say which prompt to change", ErrInvalidProfile)
	default:
		return fmt.Errorf("%w: unknown prompt %q", ErrInvalidProfile, op.Field)
	}
	return nil
}

func appendText(cur, add string) string {
	add = strings.TrimSpace(add)
	if add == "" {
		return cur
	}
	if strings.TrimSpace(cur) == "" {
		return add
	}
	return strings.TrimRight(cur, "\n") + "\n" + add
}

func (p Profile) findSection(op ProfileOp) (int, error) {
	if id := strings.TrimSpace(op.SectionID); id != "" {
		for i, s := range p.Sections {
			if s.ID == id {
				return i, nil
			}
		}
		return 0, fmt.Errorf("%w: no prompt with id %q", ErrInvalidProfile, id)
	}
	title := strings.TrimSpace(cmp.Or(op.SectionTitle, op.NewTitle))
	if title == "" {
		return 0, fmt.Errorf("%w: say which added prompt, by id or title", ErrInvalidProfile)
	}
	found := -1
	for i, s := range p.Sections {
		if strings.EqualFold(s.Title, title) {
			if found >= 0 {
				return 0, fmt.Errorf("%w: more than one prompt is titled %q; use its id", ErrInvalidProfile, title)
			}
			found = i
		}
	}
	if found < 0 {
		return 0, fmt.Errorf("%w: no prompt titled %q", ErrInvalidProfile, title)
	}
	return found, nil
}

func (p Profile) snapshot(title string) *ProfileRevision {
	sections := append([]ProfileSection(nil), p.Sections...)
	return &ProfileRevision{
		At: time.Now().UTC(), Title: title, Name: p.Name, Need: p.Need, Exclude: p.Exclude,
		Scoring: string(p.ScoringText), Sections: sections,
	}
}

// promptBody is the three prompts and the added ones, in the order the agents read.
func (p Profile) promptBody() string {
	var b strings.Builder
	write := func(heading, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		fmt.Fprintf(&b, "## %s\n%s\n\n", heading, text)
	}
	write("Need", p.Need)
	write("Exclude", p.Exclude)
	write("Jev score", string(p.ScoringText))
	for _, s := range p.Sections {
		write(s.Title, s.Body)
	}
	return strings.TrimRight(b.String(), "\n")
}
