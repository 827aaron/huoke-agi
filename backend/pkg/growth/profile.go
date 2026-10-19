package growth

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"pentagi/pkg/database"
	"pentagi/pkg/docker"
	"pentagi/pkg/flowfiles"
	"pentagi/pkg/graph/model"
	"pentagi/pkg/growth/screening"
	"pentagi/pkg/resources"

	"github.com/sirupsen/logrus"
)

// ProfileResourcePath is the file of the built-in default profile in a flow container,
// under {{.Cwd}}/resources; a saved profile's file carries its id.
const ProfileResourcePath = "business_profile.md"

// profileGuideMarker is the description of the guide documents a profile owns; saving
// the profile replaces exactly those documents and no others.
const profileGuideMarker = "growth business profile"

// compactRunes bounds the prompt block carried on every agent call; the file holds the rest.
const compactRunes = 240

var (
	ErrInvalidProfile = errors.New("invalid business profile")
	ErrNoProfile      = errors.New("no business profile is saved")
	errCorruptProfile = errors.New("stored business profile is corrupt")

	profileBlock = regexp.MustCompile(`(?s)<business_profile>.*?</business_profile>\s*`)
)

// Profile is the three prompts the agents and Jev read, plus any prompts the owner added.
// Need says who to look for, Exclude who scores lower, ScoringText how Jev's answers become
// a score. The structured fields are the older shape: a saved profile or a Go value may still
// set them, and normalize copies them into the three prompts when those are empty, then reads
// the prompts back so the judgement code can keep using them.
type Profile struct {
	Name        string           `json:"name"`
	Need        string           `json:"need,omitempty"`
	Exclude     string           `json:"exclude,omitempty"`
	ScoringText scoringText      `json:"scoring,omitempty"`
	Sections    []ProfileSection `json:"sections,omitempty"`
	// Legacy is the profile document from before the three prompts, kept so the migration
	// can be reversed. A profile created after the migration leaves it empty.
	Legacy json.RawMessage `json:"legacy,omitempty" jsonschema:"-"`
	// Editor is "assistant" or "user", whoever saved last. Revision is the document from
	// before the last assistant edit.
	Editor   string           `json:"editor,omitempty" jsonschema:"-"`
	Revision *ProfileRevision `json:"revision,omitempty" jsonschema:"-"`

	Pitch           string   `json:"pitch,omitempty" jsonschema:"-"`
	Games           []string `json:"games,omitempty" jsonschema:"-"`
	Genre           string   `json:"genre,omitempty" jsonschema:"-"`
	Competitors     []string `json:"competitors,omitempty" jsonschema:"-"`
	Regions         []string `json:"regions,omitempty" jsonschema:"-"`
	Languages       []string `json:"languages,omitempty" jsonschema:"-"`
	DefaultTarget   int      `json:"default_target,omitempty" jsonschema:"-"`
	Signals         []string `json:"signals,omitempty" jsonschema:"-"`
	Unwanted        []string `json:"unwanted,omitempty" jsonschema:"-"`
	UnwantedPenalty int      `json:"unwanted_penalty,omitempty" jsonschema:"-"`
	Notes           string   `json:"notes,omitempty" jsonschema:"-"`
	// Scoring is the parsed Jev rules; nil means they were not parsed. JSON carries the text.
	Scoring      *Scoring `json:"-" jsonschema:"-"`
	ContactTypes []string `json:"contact_types,omitempty" jsonschema:"-"`

	// LegacyExcludes is the field Unwanted replaced; a stored profile or an older page may
	// still carry it, and normalize moves it over.
	LegacyExcludes []string `json:"excludes,omitempty" jsonschema:"-"`
}

// ComplianceExcludes are dropped whatever the profile says, and only on explicit evidence
// in the message: they are the line no customer's targeting may cross.
var ComplianceExcludes = []string{
	"a stated age under 18",
	"an explicit request for help to stop gambling, or a stated self-exclusion",
}

// complianceWords mark an item of an older profile's exclusions that ComplianceExcludes
// now covers, so moving it to Unwanted would only weaken it.
var complianceWords = []string{"under 18", "underage", "under-age", "minor", "stop gambling", "self-exclu", "未成年", "戒赌"}

const DefaultUnwantedPenalty = 3

// Scoring weighs a message: each dimension the judgement model answers yes to adds its
// points, and Recent adds its points for a message of the last 7 days and half of them for
// one of the last 30. A score of Qualified or more counts toward the target; from
// Watchlist up a lead is kept for review; below it is dropped.
type Scoring struct {
	Dimensions []ScoringDimension `json:"dimensions"`
	// Recent is judged by the message time, not asked of the model; nil adds nothing.
	Recent    *RecentDimension `json:"recent,omitempty"`
	Qualified int              `json:"qualified"`
	Watchlist int              `json:"watchlist"`

	// LegacyWeights is the fixed five-signal form Dimensions replaced.
	LegacyWeights *ScoringWeights `json:"weights,omitempty" jsonschema:"-"`
}

// ScoringDimension is one yes/no question the judgement model is asked of a message.
// Key is a stable slug the answers and the score breakdown are keyed by; Description is
// the English text the model reads as what counts as yes.
type ScoringDimension struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Points      int    `json:"points"`
}

type RecentDimension struct {
	Points int `json:"points"`
}

type ScoringWeights struct {
	Player int `json:"player"`
	Pain   int `json:"pain"`
	Paying int `json:"paying"`
	Active int `json:"active"`
	Match  int `json:"match"`
}

// maxDimensionPoints bounds one dimension so a typo cannot make a single answer qualify
// every message.
const maxDimensionPoints = 20

// defaultDimensions are the four questions the judgement model has always been asked,
// keyed as its answers are.
func defaultDimensions(player, pain, paying, match int) []ScoringDimension {
	points := map[string]int{
		screening.SignalPlayer: player, screening.SignalPain: pain, screening.SignalPaying: paying, screening.SignalMatch: match,
	}
	dims := make([]ScoringDimension, 0, len(screening.Signals))
	for _, sig := range screening.Signals {
		dims = append(dims, ScoringDimension{Key: sig.Key, Name: sig.Name, Description: sig.Description, Points: points[sig.Key]})
	}
	return dims
}

// DefaultScoring weighs an offer with no weighting of its own.
func DefaultScoring() Scoring {
	return Scoring{
		Dimensions: defaultDimensions(3, 3, 2, 3),
		Recent:     &RecentDimension{Points: 2},
		Qualified:  QualifiedMinScore,
		Watchlist:  WatchlistMinScore,
	}
}

func (sc Scoring) maxScore() int {
	total := 0
	for _, d := range sc.Dimensions {
		total += d.Points
	}
	if sc.Recent != nil {
		total += sc.Recent.Points
	}
	return total
}

// The contact types a lead can carry. A platform handle is the lead's own account, which
// every lead has.
const (
	ContactPlatformHandle = "platform_handle"
	ContactPhone          = "phone"
	ContactEmail          = "email"
	ContactWhatsApp       = "whatsapp"
	ContactLine           = "line"
	ContactTelegram       = "telegram"
	ContactOther          = "other"
)

var ContactTypes = []string{
	ContactPlatformHandle, ContactPhone, ContactEmail, ContactWhatsApp, ContactLine, ContactTelegram, ContactOther,
}

// contactMessenger is the one type that stood for every messaging app; a profile saved
// with it asks for each app that replaced it. Leads keep the contacts stored under it.
const contactMessenger = "messenger"

var messengerApps = []string{ContactWhatsApp, ContactLine, ContactTelegram}

// A contact type outside ContactTypes is a name the owner chose, such as "WeChat"; it
// is kept as written and only its length is limited.
const maxContactTypeRunes = 30

// contactType is t in canonical form: a built-in type in lower case, any other name as
// written.
func contactType(t string) string {
	if lower := strings.ToLower(t); slices.Contains(ContactTypes, lower) || lower == contactMessenger {
		return lower
	}
	return t
}

// ScoringOrDefault is the profile's scoring, or DefaultScoring when it sets none.
func (p Profile) ScoringOrDefault() Scoring {
	if p.Scoring != nil && len(p.Scoring.Dimensions) > 0 {
		return *p.Scoring
	}
	if sc, _, err := ParseScoring(string(p.ScoringText)); err == nil {
		return sc
	}
	return DefaultScoring()
}

func (p Profile) UnwantedPenaltyOrDefault() int {
	if p.UnwantedPenalty <= 0 {
		return DefaultUnwantedPenalty
	}
	return p.UnwantedPenalty
}

// ProfileView is a stored profile. ID 0 is DefaultProfile standing in for a customer who
// saved none. Knowledge is "seeded", "skipped: <reason>" or empty, and only set by a save.
// EditedBy is who saved last; HasPrevious is set when an assistant edit can be restored.
type ProfileView struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Default     bool   `json:"default"`
	EditedBy    string `json:"edited_by,omitempty"`
	HasPrevious bool   `json:"has_previous,omitempty"`
	Profile
	UpdatedAt *time.Time `json:"updated_at"`
	Knowledge string     `json:"knowledge,omitempty"`
}

// ProfileInput is a profile with the name the owner knows it by; an empty title is the
// business name.
type ProfileInput struct {
	Title string `json:"title"`
	Profile
}

// ProfileKnowledge is the slice of the knowledge store a profile writes its long lists to.
type ProfileKnowledge interface {
	ListUserDocuments(ctx context.Context, userID int64, filter *model.KnowledgeFilter, withContent bool) ([]*model.KnowledgeDocument, error)
	CreateDocument(ctx context.Context, userID int64, input model.CreateKnowledgeDocumentInput) (*model.KnowledgeDocument, error)
	DeleteUserDocument(ctx context.Context, userID int64, id string) error
}

func (s *Service) SetKnowledgeStore(k ProfileKnowledge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.knowledge = k
}

func trimItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func (p *Profile) normalize() {
	p.Name = strings.TrimSpace(p.Name)
	p.Pitch = strings.TrimSpace(p.Pitch)
	p.Genre = strings.TrimSpace(p.Genre)
	p.Notes = strings.TrimSpace(p.Notes)
	p.Games, p.Competitors = trimItems(p.Games), trimItems(p.Competitors)
	p.Regions, p.Languages = trimItems(p.Regions), trimItems(p.Languages)
	p.Signals, p.Unwanted = trimItems(p.Signals), trimItems(p.Unwanted)
	if len(p.Unwanted) == 0 {
		for _, item := range trimItems(p.LegacyExcludes) {
			lower := strings.ToLower(item)
			if !slices.ContainsFunc(complianceWords, func(w string) bool { return strings.Contains(lower, w) }) {
				p.Unwanted = append(p.Unwanted, item)
			}
		}
	}
	p.LegacyExcludes = nil
	if p.Scoring != nil {
		sc := *p.Scoring
		sc.normalize()
		p.Scoring = &sc
	}
	p.canonContacts()
	_ = p.foldTexts()
	p.canonContacts()
}

// WithPrompts fills the three prompts from the structured fields. A preset is stored
// that way, and the profile form pastes the prompts in as text.
func (p Profile) WithPrompts() Profile {
	p.normalize()
	return p
}

func (p *Profile) canonContacts() {
	types := make([]string, 0, len(p.ContactTypes))
	for _, t := range trimItems(p.ContactTypes) {
		expanded := []string{contactType(t)}
		if expanded[0] == contactMessenger {
			expanded = messengerApps
		}
		for _, t := range expanded {
			if !slices.ContainsFunc(types, func(have string) bool { return strings.EqualFold(have, t) }) {
				types = append(types, t)
			}
		}
	}
	if len(types) == 0 {
		types = []string{ContactPlatformHandle}
	}
	p.ContactTypes = types
}

var dimensionKeyChars = regexp.MustCompile(`[^a-z0-9]+`)

// normalize converts the legacy weights and gives every dimension a unique key: its own,
// else a slug of its name, else dN for a name with no Latin letters.
func (sc *Scoring) normalize() {
	if w := sc.LegacyWeights; w != nil && len(sc.Dimensions) == 0 {
		sc.Dimensions = defaultDimensions(w.Player, w.Pain, w.Paying, w.Match)
		if sc.Recent == nil && w.Active > 0 {
			sc.Recent = &RecentDimension{Points: w.Active}
		}
	}
	sc.LegacyWeights = nil
	dims := make([]ScoringDimension, 0, len(sc.Dimensions))
	seen := map[string]bool{}
	for _, d := range sc.Dimensions {
		d.Name, d.Description = strings.TrimSpace(d.Name), strings.TrimSpace(d.Description)
		if d.Name == "" && d.Description == "" {
			continue
		}
		key := strings.Trim(dimensionKeyChars.ReplaceAllString(strings.ToLower(cmp.Or(d.Key, d.Name)), "_"), "_")
		if key == "" || key == signalActive || key == LeadFlagUnwanted || seen[key] {
			for n := len(dims) + 1; ; n++ {
				if key = fmt.Sprintf("d%d", n); !seen[key] {
					break
				}
			}
		}
		seen[key] = true
		d.Key = key
		dims = append(dims, d)
	}
	sc.Dimensions = dims
}

func (p Profile) validate() error {
	if err := (&p).limits(); err != nil {
		return err
	}
	if strings.TrimSpace(string(p.ScoringText)) != "" {
		if _, _, err := ParseScoring(string(p.ScoringText)); err != nil {
			return err
		}
	}
	switch {
	case p.Name == "" && p.Pitch == "" && len(p.Games) == 0 && strings.TrimSpace(p.Need) == "" &&
		strings.TrimSpace(p.Exclude) == "" && len(p.Sections) == 0:
		return fmt.Errorf("%w: say what to look for, or name the business", ErrInvalidProfile)
	case p.DefaultTarget < 0:
		return fmt.Errorf("%w: default_target cannot be negative", ErrInvalidProfile)
	case p.UnwantedPenalty < 0:
		return fmt.Errorf("%w: unwanted_penalty cannot be negative", ErrInvalidProfile)
	}
	for _, t := range p.ContactTypes {
		if utf8.RuneCountInString(t) > maxContactTypeRunes {
			return fmt.Errorf("%w: contact type %q is longer than %d characters", ErrInvalidProfile, t, maxContactTypeRunes)
		}
	}
	if sc := p.Scoring; sc != nil {
		if len(sc.Dimensions) == 0 {
			return fmt.Errorf("%w: scoring needs at least one dimension", ErrInvalidProfile)
		}
		for _, d := range sc.Dimensions {
			if d.Name == "" {
				return fmt.Errorf("%w: every scoring dimension needs a name", ErrInvalidProfile)
			}
			if d.Points < 0 || d.Points > maxDimensionPoints {
				return fmt.Errorf("%w: dimension %q: points are between 0 and %d", ErrInvalidProfile, d.Name, maxDimensionPoints)
			}
		}
		if sc.Recent != nil && (sc.Recent.Points < 0 || sc.Recent.Points > maxDimensionPoints) {
			return fmt.Errorf("%w: recent points are between 0 and %d", ErrInvalidProfile, maxDimensionPoints)
		}
		if sc.Watchlist < 1 || sc.Qualified <= sc.Watchlist {
			return fmt.Errorf("%w: scoring needs 1 <= watchlist < qualified", ErrInvalidProfile)
		}
		if sc.maxScore() < sc.Qualified {
			return fmt.Errorf("%w: the points add up to less than qualified, so no lead could qualify", ErrInvalidProfile)
		}
	}
	return nil
}

func profileView(row database.GrowthProfile) (ProfileView, error) {
	v := ProfileView{
		ID: row.ID, Title: row.Title, Default: row.IsDefault,
		UpdatedAt: nullTime(row.UpdatedAt.Time, row.UpdatedAt.Valid),
	}
	if err := json.Unmarshal(row.Profile, &v.Profile); err != nil {
		return ProfileView{}, fmt.Errorf("%w: %w", errCorruptProfile, err)
	}
	v.normalize()
	v.EditedBy = v.Editor
	v.HasPrevious = v.Revision != nil
	return v, nil
}

// builtInProfileView is DefaultProfile for a customer with no saved profile.
func builtInProfileView() ProfileView {
	return ProfileView{Default: true, Profile: DefaultProfile()}
}

func (s *Service) GetProfile(ctx context.Context, userID, id int64) (ProfileView, error) {
	row, err := s.db.GetUserGrowthProfile(ctx, database.GetUserGrowthProfileParams{ID: id, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return ProfileView{}, ErrNoProfile
	}
	if err != nil {
		return ProfileView{}, fmt.Errorf("failed to load the business profile: %w", err)
	}
	return profileView(row)
}

// DefaultProfileOf is the owner's default profile, or ErrNoProfile when they saved none.
func (s *Service) DefaultProfileOf(ctx context.Context, userID int64) (ProfileView, error) {
	row, err := s.db.GetDefaultGrowthProfile(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ProfileView{}, ErrNoProfile
	}
	if err != nil {
		return ProfileView{}, fmt.Errorf("failed to load the business profile: %w", err)
	}
	return profileView(row)
}

// ListProfiles returns the owner's profiles, the default first.
func (s *Service) ListProfiles(ctx context.Context, userID int64) ([]ProfileView, error) {
	rows, err := s.db.GetUserGrowthProfiles(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load the business profiles: %w", err)
	}
	out := make([]ProfileView, 0, len(rows))
	for _, row := range rows {
		v, err := profileView(row)
		if err != nil {
			logrus.WithContext(ctx).WithError(err).WithField("profile_id", row.ID).Warn("business profile left out of the list")
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func (in *ProfileInput) prepare() ([]byte, error) {
	in.normalize()
	if err := in.validate(); err != nil {
		return nil, err
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		in.Title = cmp.Or(in.Name, strings.Join(in.Games, ", "))
	}
	return json.Marshal(in.Profile)
}

// CreateProfile stores a new profile; a customer's first profile is their default.
func (s *Service) CreateProfile(ctx context.Context, userID int64, in ProfileInput) (ProfileView, error) {
	raw, err := in.prepare()
	if err != nil {
		return ProfileView{}, err
	}
	row, err := s.db.CreateGrowthProfile(ctx, database.CreateGrowthProfileParams{UserID: userID, Title: in.Title, Profile: raw})
	if err != nil {
		return ProfileView{}, fmt.Errorf("failed to store the business profile: %w", err)
	}
	return s.savedProfile(ctx, userID, row)
}

// UpdateProfile saves over one of the owner's profiles. A change to what the judgement
// model reads or to the scoring drops the message scores judged by that profile: they were
// judged for another audience, and a message saved as evidence later is judged again.
// Leads already saved keep their scores.
func (s *Service) UpdateProfile(ctx context.Context, userID, id int64, in ProfileInput) (ProfileView, error) {
	return s.writeProfile(ctx, userID, id, in, false)
}

// UpdateProfilePrompt applies one assistant edit and keeps the previous prompts so the
// owner can restore them. id 0 is the owner's default profile.
func (s *Service) UpdateProfilePrompt(ctx context.Context, userID, id int64, op ProfileOp) (ProfileView, error) {
	cur, err := s.profileToEdit(ctx, userID, id)
	if err != nil {
		return ProfileView{}, err
	}
	rev := cur.snapshot(cur.Title)
	next := cur.Profile
	next.Sections = append([]ProfileSection(nil), next.Sections...)
	if err := next.apply(op); err != nil {
		return ProfileView{}, err
	}
	next.Revision = rev
	next.Editor = editorAssistant
	next.Legacy = cur.Legacy
	title := cur.Title
	if t := strings.TrimSpace(op.ProfileTitle); t != "" {
		title = t
	}
	return s.writeProfile(ctx, userID, cur.ID, ProfileInput{Title: title, Profile: next}, true)
}

// RestoreProfile puts back the prompts from before the last assistant edit.
func (s *Service) RestoreProfile(ctx context.Context, userID, id int64) (ProfileView, error) {
	cur, err := s.GetProfile(ctx, userID, id)
	if err != nil {
		return ProfileView{}, err
	}
	if cur.Revision == nil {
		return ProfileView{}, fmt.Errorf("%w: this profile has no earlier version", ErrInvalidProfile)
	}
	rev := cur.Revision
	next := Profile{
		Name: rev.Name, Need: rev.Need, Exclude: rev.Exclude, ScoringText: scoringText(rev.Scoring),
		Sections: rev.Sections, Legacy: cur.Legacy, Editor: editorUser,
	}
	title := cmp.Or(strings.TrimSpace(rev.Title), cur.Title)
	return s.writeProfile(ctx, userID, id, ProfileInput{Title: title, Profile: next}, true)
}

func (s *Service) profileToEdit(ctx context.Context, userID, id int64) (ProfileView, error) {
	if id == 0 {
		v, err := s.DefaultProfileOf(ctx, userID)
		if errors.Is(err, ErrNoProfile) {
			return ProfileView{}, fmt.Errorf("%w: save a profile before changing one", ErrNoProfile)
		}
		return v, err
	}
	return s.GetProfile(ctx, userID, id)
}

// keepRevision is set when the caller already put the history it wants on the profile.
func (s *Service) writeProfile(ctx context.Context, userID, id int64, in ProfileInput, keepRevision bool) (ProfileView, error) {
	prev, err := s.GetProfile(ctx, userID, id)
	if errors.Is(err, errCorruptProfile) {
		prev = builtInProfileView()
	} else if err != nil {
		return ProfileView{}, err
	}
	if len(in.Legacy) == 0 {
		in.Legacy = prev.Legacy
	}
	if !keepRevision {
		in.Editor = editorUser
		if in.Revision == nil {
			in.Revision = prev.Revision
		}
	}
	raw, err := in.prepare()
	if err != nil {
		return ProfileView{}, err
	}
	var row database.GrowthProfile
	if businessSummary(prev.Profile) != businessSummary(in.Profile) || !sameScoring(prev.Profile, in.Profile) {
		row, err = s.db.UpdateGrowthProfileDroppingScores(ctx, database.UpdateGrowthProfileDroppingScoresParams{
			ID: id, UserID: userID, Title: in.Title, Profile: raw,
		})
		if err == nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{"user_id": userID, "profile_id": id}).
				Info("business profile changed what leads are judged by; its stored message scores dropped")
		}
	} else {
		row, err = s.db.UpdateGrowthProfile(ctx, database.UpdateGrowthProfileParams{ID: id, UserID: userID, Title: in.Title, Profile: raw})
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ProfileView{}, ErrNoProfile
	}
	if err != nil {
		return ProfileView{}, fmt.Errorf("failed to store the business profile: %w", err)
	}
	return s.savedProfile(ctx, userID, row)
}

func sameScoring(a, b Profile) bool {
	x, _ := json.Marshal(a.ScoringOrDefault())
	y, _ := json.Marshal(b.ScoringOrDefault())
	return bytes.Equal(x, y) && a.UnwantedPenaltyOrDefault() == b.UnwantedPenaltyOrDefault() &&
		slices.Equal(a.Unwanted, b.Unwanted)
}

// savedProfile reseeds the knowledge store when the default profile changed: the store
// holds the default profile's lists only.
func (s *Service) savedProfile(ctx context.Context, userID int64, row database.GrowthProfile) (ProfileView, error) {
	v, err := profileView(row)
	if err != nil {
		return ProfileView{}, err
	}
	if v.Default {
		v.Knowledge = s.seedProfileKnowledge(ctx, userID, &v.Profile)
	}
	return v, nil
}

// SetDefaultProfile makes the profile new tasks start with when none is chosen.
func (s *Service) SetDefaultProfile(ctx context.Context, userID, id int64) (ProfileView, error) {
	n, err := s.db.SetDefaultGrowthProfile(ctx, database.SetDefaultGrowthProfileParams{ID: id, UserID: userID})
	if err != nil {
		return ProfileView{}, fmt.Errorf("failed to set the default business profile: %w", err)
	}
	if n == 0 {
		return ProfileView{}, ErrNoProfile
	}
	v, err := s.GetProfile(ctx, userID, id)
	if err != nil {
		return ProfileView{}, err
	}
	v.Knowledge = s.seedProfileKnowledge(ctx, userID, &v.Profile)
	return v, nil
}

// DeleteProfile removes one of the owner's profiles with the message scores judged by it.
// When it was the default, the oldest remaining profile becomes the default; flows that
// used it fall back to the default.
func (s *Service) DeleteProfile(ctx context.Context, userID, id int64) error {
	row, err := s.db.DeleteGrowthProfile(ctx, database.DeleteGrowthProfileParams{ID: id, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoProfile
	}
	if err != nil {
		return fmt.Errorf("failed to delete the business profile: %w", err)
	}
	if !row.IsDefault {
		return nil
	}
	promoted, err := s.db.PromoteGrowthProfile(ctx, userID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		s.seedProfileKnowledge(ctx, userID, nil)
		return nil
	case err != nil:
		return fmt.Errorf("failed to choose the next default business profile: %w", err)
	}
	if v, err := profileView(promoted); err == nil {
		s.seedProfileKnowledge(ctx, userID, &v.Profile)
	}
	return nil
}

type selectedProfileKey struct{}

// WithSelectedProfile carries the profile a client chose for the flow it is starting
// through the flow controller, whose signature has no place for it.
func WithSelectedProfile(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, selectedProfileKey{}, id)
}

// SelectedProfile is the profile WithSelectedProfile put in ctx.
func SelectedProfile(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(selectedProfileKey{}).(int64)
	return id, ok
}

// ResolveProfile is the profile a new flow starts with: the selected one, which must be
// the owner's, else the owner's default, else DefaultProfile with ID 0.
func (s *Service) ResolveProfile(ctx context.Context, userID int64, selected *int64) (ProfileView, error) {
	if selected != nil {
		return s.GetProfile(ctx, userID, *selected)
	}
	v, err := s.DefaultProfileOf(ctx, userID)
	if errors.Is(err, ErrNoProfile) || errors.Is(err, errCorruptProfile) {
		return builtInProfileView(), nil
	}
	return v, err
}

// BindFlowProfile records the profile a flow started with, so its follow-up tasks and the
// judgement of its leads keep using it when the default changes.
func (s *Service) BindFlowProfile(ctx context.Context, flowID int64, v ProfileView) error {
	params := database.UpsertGrowthFlowProfileParams{FlowID: flowID, Title: v.Title}
	if v.ID != 0 {
		params.ProfileID = sql.NullInt64{Int64: v.ID, Valid: true}
	}
	if err := s.db.UpsertGrowthFlowProfile(ctx, params); err != nil {
		return fmt.Errorf("failed to record the business profile of flow %d: %w", flowID, err)
	}
	return nil
}

// ProfileForFlow is the profile a flow's tasks and lead judgement use: the one it was
// started with while it exists, else the owner's default, else DefaultProfile with ID 0.
// flowID 0 is a call outside any flow.
func (s *Service) ProfileForFlow(ctx context.Context, userID, flowID int64) (ProfileView, error) {
	if flowID != 0 {
		fp, err := s.db.GetGrowthFlowProfile(ctx, flowID)
		switch {
		case err == nil && fp.ProfileID.Valid:
			v, err := s.GetProfile(ctx, userID, fp.ProfileID.Int64)
			if !errors.Is(err, ErrNoProfile) && !errors.Is(err, errCorruptProfile) {
				return v, err
			}
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return ProfileView{}, fmt.Errorf("failed to load the business profile of flow %d: %w", flowID, err)
		}
	}
	return s.ResolveProfile(ctx, userID, nil)
}

// FlowProfileView names the profile a flow uses. Fallback is true when the flow recorded
// none, or the one it recorded is deleted, so the default stands in.
type FlowProfileView struct {
	FlowID    int64  `json:"flow_id"`
	ProfileID *int64 `json:"profile_id"`
	Title     string `json:"title"`
	Fallback  bool   `json:"fallback"`
}

func (s *Service) FlowProfile(ctx context.Context, userID, flowID int64) (FlowProfileView, error) {
	if _, err := s.db.GetUserFlow(ctx, database.GetUserFlowParams{ID: flowID, UserID: userID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FlowProfileView{}, fmt.Errorf("%w: %d", ErrFlowNotFound, flowID)
		}
		return FlowProfileView{}, fmt.Errorf("failed to load task %d: %w", flowID, err)
	}
	v, err := s.ProfileForFlow(ctx, userID, flowID)
	if err != nil {
		return FlowProfileView{}, err
	}
	out := FlowProfileView{FlowID: flowID, Title: v.Title}
	if v.ID != 0 {
		out.ProfileID = &v.ID
	}
	fp, err := s.db.GetGrowthFlowProfile(ctx, flowID)
	out.Fallback = err != nil || !fp.ProfileID.Valid || fp.ProfileID.Int64 != v.ID
	return out, nil
}

// profileResource writes the profile file into the resource store and returns it as a
// resource at ProfileResourcePath, so a flow's copy is the file of the flow's own profile
// whichever profile it is. The resource has no row among the customer's resources.
func (s *Service) profileResource(userID int64, v ProfileView) (database.UserResource, bool, error) {
	if s.cfg.DataDir == "" {
		return database.UserResource{}, false, nil
	}
	tmpDir := filepath.Join(resources.ResourcesDir(s.cfg.DataDir), ".tmp")
	tmp, hash, size, err := resources.SaveToTemp(bytes.NewReader([]byte(v.Markdown())), tmpDir)
	if err != nil {
		return database.UserResource{}, false, err
	}
	if err := resources.CommitBlob(s.cfg.DataDir, hash, tmp); err != nil {
		_ = os.Remove(tmp)
		return database.UserResource{}, false, err
	}
	return database.UserResource{
		UserID: userID, Hash: hash, Name: v.resourcePath(), Path: v.resourcePath(), Size: size,
	}, true, nil
}

// seedProfileKnowledge replaces the profile lists in the knowledge store with p's; nil
// only removes them. The store holds the default profile's lists.
func (s *Service) seedProfileKnowledge(ctx context.Context, userID int64, p *Profile) string {
	s.mu.Lock()
	k := s.knowledge
	s.mu.Unlock()
	if k == nil {
		return "skipped: the knowledge store is not available"
	}

	guide := model.KnowledgeDocTypeGuide
	old, err := k.ListUserDocuments(ctx, userID, &model.KnowledgeFilter{DocTypes: []model.KnowledgeDocType{guide}}, false)
	if err != nil {
		return "skipped: " + err.Error()
	}
	for _, doc := range old {
		if doc.Description != nil && *doc.Description == profileGuideMarker {
			if err := k.DeleteUserDocument(ctx, userID, doc.ID); err != nil {
				return "skipped: " + err.Error()
			}
		}
	}
	if p == nil {
		return ""
	}

	subject := strings.Join(p.Games, ", ")
	if subject == "" {
		subject = p.Name
	}
	var rejected []string
	for _, c := range p.Competitors {
		rejected = append(rejected, "Competitor: "+c)
	}
	rejected = append(rejected, p.Unwanted...)
	lists := []struct {
		question string
		items    []string
	}{
		{"Signal words that mark a likely customer of " + subject, p.Signals},
		{"People, groups, competitors and phrases that make a lead less wanted for " + subject, rejected},
	}
	seeded := 0
	for _, l := range lists {
		if len(l.items) == 0 {
			continue
		}
		marker, other := profileGuideMarker, model.KnowledgeGuideTypeOther
		if _, err := k.CreateDocument(ctx, userID, model.CreateKnowledgeDocumentInput{
			DocType:     guide,
			Question:    l.question,
			Content:     "- " + strings.Join(l.items, "\n- "),
			Description: &marker,
			GuideType:   &other,
		}); err != nil {
			logrus.WithContext(ctx).WithError(err).WithField("user_id", userID).Warn("business profile lists not seeded")
			return "skipped: " + err.Error()
		}
		seeded++
	}
	if seeded == 0 {
		return ""
	}
	return "seeded"
}

// Compact is the block every task's input starts with. It stays short because each
// agent call carries it, summarised, in the execution context; long lists and text are
// cut here and read in full from the file or with search_guide.
func (v ProfileView) Compact() string {
	return v.compact(v.resourcePath(), v.Default)
}

// resourcePath is the file name of the profile among a flow's resources; each saved
// profile has its own, so a flow that changes profile gets the new file beside the old.
func (v ProfileView) resourcePath() string {
	if v.ID == 0 {
		return ProfileResourcePath
	}
	return fmt.Sprintf("business_profile_%d.md", v.ID)
}

// compact with no file path is the whole profile, since nothing else holds what it cuts.
// guide says the knowledge store holds this profile's lists.
func (p Profile) compact(path string, guide bool) string {
	p.Sections = append([]ProfileSection(nil), p.Sections...)
	(&p).normalize()
	withFile := path != ""
	var b strings.Builder
	cut := false
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, value)
		}
	}

	b.WriteString("<business_profile>\n")
	if title := cmp.Or(p.Name, strings.Join(p.Games, ", ")); title != "" {
		line("Name", title)
	}
	body := p.promptBody()
	if withFile {
		short := truncateRunes(body, compactRunes*4)
		if short != body {
			cut = true
			body = short
		}
	}
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") && body != "" {
		b.WriteString("\n")
	}
	line("Always excluded, on explicit evidence only", strings.Join(ComplianceExcludes, "; "))
	if withFile {
		fmt.Fprintf(&b, "Full profile: %s/%s/%s", docker.WorkFolderPathInContainer, flowfiles.ResourcesDirName, path)
		switch {
		case guide:
			b.WriteString(" (read the file, or search_guide \"business profile\")")
		case cut:
			b.WriteString(" (the lines above are cut; read the file)")
		}
		b.WriteString("\n")
	}
	b.WriteString("</business_profile>")
	return b.String()
}

// Markdown is the whole profile as the resource file holds it.
func (p Profile) Markdown() string {
	p.Sections = append([]ProfileSection(nil), p.Sections...)
	(&p).normalize()
	var b strings.Builder
	title := p.Name
	if title == "" {
		title = strings.Join(p.Games, ", ")
	}
	fmt.Fprintf(&b, "# Business profile: %s\n\n%s\n\n", title, p.promptBody())
	fmt.Fprintf(&b, "Always excluded, on explicit evidence only: %s\n", strings.Join(ComplianceExcludes, "; "))
	return b.String()
}

// ProfileInput puts v's compact block at the start of input, replacing a block already
// there, and adds v's file to resources.
func (s *Service) ProfileInput(
	ctx context.Context,
	userID int64,
	v ProfileView,
	input string,
	res []database.UserResource,
) (string, []database.UserResource) {
	block := v.Compact()
	file, ok, err := s.profileResource(userID, v)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithField("user_id", userID).Warn("task started without the business profile file")
	}
	if !ok {
		block = v.compact("", false)
	}
	input = block + "\n\n" + strings.TrimSpace(profileBlock.ReplaceAllString(input, ""))
	if ok {
		res = withProfileFile(res, file)
	}
	return input, res
}

// ProfileResources adds v's file to res.
func (s *Service) ProfileResources(ctx context.Context, userID int64, v ProfileView, res []database.UserResource) []database.UserResource {
	file, ok, err := s.profileResource(userID, v)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithField("user_id", userID).Warn("assistant started without the business profile file")
	}
	if !ok {
		return res
	}
	return withProfileFile(res, file)
}

// withProfileFile puts file in place of any resource at its path, which would otherwise
// win the copy into the flow.
func withProfileFile(res []database.UserResource, file database.UserResource) []database.UserResource {
	out := make([]database.UserResource, 0, len(res)+1)
	for _, r := range res {
		if r.Path != file.Path {
			out = append(out, r)
		}
	}
	return append(out, file)
}

// FlowOfAssistant is the flow an assistant runs in.
func (s *Service) FlowOfAssistant(ctx context.Context, assistantID int64) (int64, error) {
	a, err := s.db.GetAssistant(ctx, assistantID)
	if err != nil {
		return 0, fmt.Errorf("failed to load assistant %d: %w", assistantID, err)
	}
	return a.FlowID, nil
}

// judgingProfile is the profile the leads of scope are judged by. A failure to load is
// returned, so a passing database error neither judges by the wrong profile nor drops scores.
func (s *Service) judgingProfile(ctx context.Context, scope Scope) (ProfileView, error) {
	return s.ProfileForFlow(ctx, scope.UserID, scope.FlowID)
}
