package growth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/docker"
	"pentagi/pkg/flowfiles"
	"pentagi/pkg/graph/model"
	"pentagi/pkg/resources"

	"github.com/sirupsen/logrus"
)

// ProfileResourcePath is where the whole profile lives among the customer's resources,
// and so in every flow container under {{.Cwd}}/resources.
const ProfileResourcePath = "business_profile.md"

// profileGuideMarker is the description of the guide documents a profile owns; saving
// the profile replaces exactly those documents and no others.
const profileGuideMarker = "growth business profile"

// compactItems and compactRunes keep each list and free text of the compact block
// short; what is cut stays in the file and the knowledge store.
const (
	compactItems = 5
	compactRunes = 240
)

var (
	ErrInvalidProfile = errors.New("invalid business profile")
	ErrNoProfile      = errors.New("no business profile is saved")

	profileBlock = regexp.MustCompile(`(?s)<business_profile>.*?</business_profile>\s*`)
)

// Profile is what a customer sells and to whom. Signals (the words that mark a likely
// customer), Excludes (who to reject) and Notes are the customer's own free text, one
// item per line; they are what tunes the generic prompts to one customer.
type Profile struct {
	Name          string   `json:"name"`
	Pitch         string   `json:"pitch"`
	Games         []string `json:"games"`
	Genre         string   `json:"genre"`
	Competitors   []string `json:"competitors"`
	Regions       []string `json:"regions"`
	Languages     []string `json:"languages"`
	DefaultTarget int      `json:"default_target"`
	Signals       []string `json:"signals"`
	Excludes      []string `json:"excludes"`
	Notes         string   `json:"notes"`
}

// ProfileView is a stored profile. A customer has one profile, so ID is the owner's id.
// Knowledge is "seeded", "skipped: <reason>" or empty, and only set by a save.
type ProfileView struct {
	ID int64 `json:"id"`
	Profile
	UpdatedAt *time.Time `json:"updated_at"`
	Knowledge string     `json:"knowledge,omitempty"`
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

func (p *Profile) normalize() {
	trim := func(items []string) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	}
	p.Name = strings.TrimSpace(p.Name)
	p.Pitch = strings.TrimSpace(p.Pitch)
	p.Genre = strings.TrimSpace(p.Genre)
	p.Notes = strings.TrimSpace(p.Notes)
	p.Games, p.Competitors = trim(p.Games), trim(p.Competitors)
	p.Regions, p.Languages = trim(p.Regions), trim(p.Languages)
	p.Signals, p.Excludes = trim(p.Signals), trim(p.Excludes)
}

func (p Profile) validate() error {
	switch {
	case p.Name == "" && p.Pitch == "" && len(p.Games) == 0:
		return fmt.Errorf("%w: name, pitch or a game is required", ErrInvalidProfile)
	case p.DefaultTarget < 0:
		return fmt.Errorf("%w: default_target cannot be negative", ErrInvalidProfile)
	}
	return nil
}

func (s *Service) GetProfile(ctx context.Context, userID int64) (ProfileView, error) {
	row, err := s.db.GetGrowthProfile(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ProfileView{}, ErrNoProfile
	}
	if err != nil {
		return ProfileView{}, fmt.Errorf("failed to load the business profile: %w", err)
	}
	return profileView(row)
}

func profileView(row database.GrowthProfile) (ProfileView, error) {
	v := ProfileView{ID: row.UserID, UpdatedAt: nullTime(row.UpdatedAt.Time, row.UpdatedAt.Valid)}
	if err := json.Unmarshal(row.Profile, &v.Profile); err != nil {
		return ProfileView{}, fmt.Errorf("stored business profile is corrupt: %w", err)
	}
	v.normalize()
	return v, nil
}

// ListProfiles returns the owner's profile, or none.
func (s *Service) ListProfiles(ctx context.Context, userID int64) ([]ProfileView, error) {
	v, err := s.GetProfile(ctx, userID)
	switch {
	case errors.Is(err, ErrNoProfile):
		return []ProfileView{}, nil
	case err != nil:
		return nil, err
	}
	return []ProfileView{v}, nil
}

// SaveProfile stores the profile, rewrites its resource file and replaces its guide
// documents. The knowledge step needs an embedder; without one it is skipped and said so.
func (s *Service) SaveProfile(ctx context.Context, userID int64, p Profile) (ProfileView, error) {
	p.normalize()
	if err := p.validate(); err != nil {
		return ProfileView{}, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return ProfileView{}, err
	}
	row, err := s.db.UpsertGrowthProfile(ctx, database.UpsertGrowthProfileParams{UserID: userID, Profile: raw})
	if err != nil {
		return ProfileView{}, fmt.Errorf("failed to store the business profile: %w", err)
	}
	if err := s.writeProfileResource(ctx, userID, p); err != nil {
		return ProfileView{}, err
	}
	v, err := profileView(row)
	if err != nil {
		return ProfileView{}, err
	}
	v.Knowledge = s.seedProfileKnowledge(ctx, userID, p)
	return v, nil
}

// UpdateProfile saves over the profile with the given id, which must be the owner's.
func (s *Service) UpdateProfile(ctx context.Context, userID, id int64, p Profile) (ProfileView, error) {
	if id != userID {
		return ProfileView{}, ErrNoProfile
	}
	if _, err := s.GetProfile(ctx, userID); err != nil {
		return ProfileView{}, err
	}
	return s.SaveProfile(ctx, userID, p)
}

func (s *Service) writeProfileResource(ctx context.Context, userID int64, p Profile) error {
	if s.cfg.DataDir == "" {
		return nil
	}
	tmpDir := filepath.Join(resources.ResourcesDir(s.cfg.DataDir), ".tmp")
	tmp, hash, size, err := resources.SaveToTemp(bytes.NewReader([]byte(p.Markdown())), tmpDir)
	if err != nil {
		return err
	}
	if err := resources.CommitBlob(s.cfg.DataDir, hash, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if _, err := s.db.UpsertUserResourceFile(ctx, database.UpsertUserResourceFileParams{
		UserID: userID, Hash: hash, Name: ProfileResourcePath, Path: ProfileResourcePath, Size: size,
	}); err != nil {
		return fmt.Errorf("failed to store %s: %w", ProfileResourcePath, err)
	}
	return nil
}

func (s *Service) seedProfileKnowledge(ctx context.Context, userID int64, p Profile) string {
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

	subject := strings.Join(p.Games, ", ")
	if subject == "" {
		subject = p.Name
	}
	var rejected []string
	for _, c := range p.Competitors {
		rejected = append(rejected, "Competitor: "+c)
	}
	rejected = append(rejected, p.Excludes...)
	lists := []struct {
		question string
		items    []string
	}{
		{"Signal words that mark a likely customer of " + subject, p.Signals},
		{"People, groups, competitors and phrases to reject for " + subject, rejected},
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
func (p Profile) Compact() string {
	var b strings.Builder
	cut := false
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, value)
		}
	}
	list := func(label string, items []string, sep string) {
		shown := head(items, compactItems)
		text := strings.Join(shown, sep)
		if len(shown) < len(items) {
			text += sep + "…"
			cut = true
		}
		line(label, text)
	}
	text := func(label, value string) {
		short := truncateRunes(value, compactRunes)
		if short != value {
			cut = true
		}
		line(label, strings.Join(strings.Fields(short), " "))
	}

	b.WriteString("<business_profile>\n")
	line("Business", p.Name)
	text("Offer", p.Pitch)
	list("Games", p.Games, ", ")
	line("Genre", p.Genre)
	list("Regions", p.Regions, ", ")
	list("Languages", p.Languages, ", ")
	if p.DefaultTarget > 0 {
		line("Lead target", fmt.Sprint(p.DefaultTarget))
	}
	list("Signal words", p.Signals, "; ")
	list("Competitors (reject)", p.Competitors, ", ")
	list("Exclude", p.Excludes, "; ")
	text("Notes", p.Notes)
	fmt.Fprintf(&b, "Full profile: %s/%s/%s", docker.WorkFolderPathInContainer, flowfiles.ResourcesDirName, ProfileResourcePath)
	if cut {
		b.WriteString(" (the lines above are cut; read the file, or search_guide \"business profile\")")
	}
	b.WriteString("\n</business_profile>")
	return b.String()
}

// Markdown is the whole profile as the resource file holds it.
func (p Profile) Markdown() string {
	var b strings.Builder
	title := p.Name
	if title == "" {
		title = strings.Join(p.Games, ", ")
	}
	fmt.Fprintf(&b, "# Business profile: %s\n\n", title)
	field := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "- **%s:** %s\n", label, value)
		}
	}
	field("Business", p.Name)
	field("Games", strings.Join(p.Games, ", "))
	field("Genre", p.Genre)
	field("Regions", strings.Join(p.Regions, ", "))
	field("Languages", strings.Join(p.Languages, ", "))
	if p.DefaultTarget > 0 {
		field("Lead target", fmt.Sprint(p.DefaultTarget))
	}
	if p.Pitch != "" {
		fmt.Fprintf(&b, "\n## Offer\n\n%s\n", p.Pitch)
	}
	section := func(heading string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n## %s\n\n- %s\n", heading, strings.Join(items, "\n- "))
	}
	section("Signal words", p.Signals)
	section("Competitors (reject their staff and promoters)", p.Competitors)
	section("Exclude", p.Excludes)
	if p.Notes != "" {
		fmt.Fprintf(&b, "\n## Notes\n\n%s\n", p.Notes)
	}
	return b.String()
}

func head(items []string, n int) []string {
	if len(items) > n {
		return items[:n]
	}
	return items
}

// WithProfile puts the owner's compact profile at the start of input, replacing a block
// already there, and adds the profile file to resources. Without a saved profile both
// come back unchanged.
func (s *Service) WithProfile(
	ctx context.Context,
	userID int64,
	input string,
	res []database.UserResource,
) (string, []database.UserResource) {
	v, err := s.GetProfile(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrNoProfile) {
			logrus.WithContext(ctx).WithError(err).WithField("user_id", userID).Warn("task started without the business profile")
		}
		return input, res
	}
	input = v.Compact() + "\n\n" + strings.TrimSpace(profileBlock.ReplaceAllString(input, ""))
	return input, s.WithProfileResource(ctx, userID, res)
}

// WithProfileResource adds the profile file to res unless it is there already.
func (s *Service) WithProfileResource(ctx context.Context, userID int64, res []database.UserResource) []database.UserResource {
	file, err := s.db.GetUserResourceByPath(ctx, database.GetUserResourceByPathParams{UserID: userID, Path: ProfileResourcePath})
	if err != nil {
		return res
	}
	for _, r := range res {
		if r.ID == file.ID {
			return res
		}
	}
	return append(res, file)
}
