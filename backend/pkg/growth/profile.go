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

const (
	compactSignals = 5
	compactSources = 10
)

var (
	ErrInvalidProfile = errors.New("invalid business profile")
	ErrNoProfile      = errors.New("no business profile is saved")

	profileBlock = regexp.MustCompile(`(?s)<business_profile>.*?</business_profile>\s*`)
)

// Profile is what a customer sells and to whom. The short fields are rendered into every
// task's input; the three long lists only go to the resource file and the knowledge store.
type Profile struct {
	BusinessName string   `json:"business_name"`
	Product      string   `json:"product"`
	Audience     string   `json:"audience"`
	Regions      []string `json:"regions,omitempty"`
	Languages    []string `json:"languages,omitempty"`
	TargetLeads  int      `json:"target_leads,omitempty"`
	Signals      []string `json:"signals,omitempty"`
	Sources      []string `json:"sources,omitempty"`
	Notes        string   `json:"notes,omitempty"`

	SignalWords     []string `json:"signal_words,omitempty"`
	BadActorSamples []string `json:"bad_actor_samples,omitempty"`
	Exclusions      []string `json:"exclusions,omitempty"`
}

// ProfileKnowledge is the slice of the knowledge store a profile writes its long lists to.
type ProfileKnowledge interface {
	ListUserDocuments(ctx context.Context, userID int64, filter *model.KnowledgeFilter, withContent bool) ([]*model.KnowledgeDocument, error)
	CreateDocument(ctx context.Context, userID int64, input model.CreateKnowledgeDocumentInput) (*model.KnowledgeDocument, error)
	DeleteUserDocument(ctx context.Context, userID int64, id string) error
}

// SavedProfile is a stored profile with what saving it did to the knowledge store:
// Knowledge is "seeded", "skipped: <reason>" or empty when there was nothing to seed.
type SavedProfile struct {
	Profile   Profile `json:"profile"`
	Knowledge string  `json:"knowledge,omitempty"`
}

func (s *Service) SetKnowledgeStore(k ProfileKnowledge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.knowledge = k
}

func (p *Profile) normalize() {
	trim := func(items []string) []string {
		out := items[:0]
		for _, item := range items {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	}
	p.BusinessName = strings.TrimSpace(p.BusinessName)
	p.Product = strings.TrimSpace(p.Product)
	p.Audience = strings.TrimSpace(p.Audience)
	p.Notes = strings.TrimSpace(p.Notes)
	p.Regions, p.Languages = trim(p.Regions), trim(p.Languages)
	p.Signals, p.Sources = trim(p.Signals), trim(p.Sources)
	p.SignalWords, p.BadActorSamples, p.Exclusions = trim(p.SignalWords), trim(p.BadActorSamples), trim(p.Exclusions)
}

func (p Profile) validate() error {
	switch {
	case p.BusinessName == "" && p.Product == "":
		return fmt.Errorf("%w: business_name or product is required", ErrInvalidProfile)
	case p.TargetLeads < 0:
		return fmt.Errorf("%w: target_leads cannot be negative", ErrInvalidProfile)
	}
	return nil
}

func (s *Service) GetProfile(ctx context.Context, userID int64) (Profile, error) {
	row, err := s.db.GetGrowthProfile(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNoProfile
	}
	if err != nil {
		return Profile{}, fmt.Errorf("failed to load the business profile: %w", err)
	}
	var p Profile
	if err := json.Unmarshal(row.Profile, &p); err != nil {
		return Profile{}, fmt.Errorf("stored business profile is corrupt: %w", err)
	}
	return p, nil
}

// SaveProfile stores the profile, rewrites its resource file and replaces its guide
// documents. The knowledge step needs an embedder; without one it is skipped and said so.
func (s *Service) SaveProfile(ctx context.Context, userID int64, p Profile) (SavedProfile, error) {
	p.normalize()
	if err := p.validate(); err != nil {
		return SavedProfile{}, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return SavedProfile{}, err
	}
	if _, err := s.db.UpsertGrowthProfile(ctx, database.UpsertGrowthProfileParams{UserID: userID, Profile: raw}); err != nil {
		return SavedProfile{}, fmt.Errorf("failed to store the business profile: %w", err)
	}
	if err := s.writeProfileResource(ctx, userID, p); err != nil {
		return SavedProfile{}, err
	}
	return SavedProfile{Profile: p, Knowledge: s.seedProfileKnowledge(ctx, userID, p)}, nil
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

	subject := p.Product
	if subject == "" {
		subject = p.BusinessName
	}
	lists := []struct {
		question string
		items    []string
	}{
		{"Signal words that mark a likely customer of " + subject, p.SignalWords},
		{"Bad actors to reject for " + subject + ": account sellers, boosting services, competitors, bots", p.BadActorSamples},
		{"People, groups and phrases excluded for " + subject, p.Exclusions},
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
// agent call carries it, summarised, in the execution context.
func (p Profile) Compact() string {
	var b strings.Builder
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, value)
		}
	}
	b.WriteString("<business_profile>\n")
	line("Business", p.BusinessName)
	line("Product", p.Product)
	line("Target customers", p.Audience)
	line("Regions", strings.Join(p.Regions, ", "))
	line("Languages", strings.Join(p.Languages, ", "))
	if p.TargetLeads > 0 {
		line("Lead target", fmt.Sprint(p.TargetLeads))
	}
	line("Custom signals", strings.Join(head(p.Signals, compactSignals), "; "))
	line("Sources to read", strings.Join(head(p.Sources, compactSources), " "))
	fmt.Fprintf(&b, "Full profile: %s/%s/%s", docker.WorkFolderPathInContainer, flowfiles.ResourcesDirName, ProfileResourcePath)
	if len(p.SignalWords)+len(p.BadActorSamples)+len(p.Exclusions) > 0 {
		b.WriteString("; signal words, bad-actor samples and exclusions: search_guide \"business profile\"")
	}
	b.WriteString("\n</business_profile>")
	return b.String()
}

// Markdown is the whole profile as the resource file holds it.
func (p Profile) Markdown() string {
	var b strings.Builder
	title := p.BusinessName
	if title == "" {
		title = p.Product
	}
	fmt.Fprintf(&b, "# Business profile: %s\n\n", title)
	field := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "- **%s:** %s\n", label, value)
		}
	}
	field("Business", p.BusinessName)
	field("Product", p.Product)
	field("Target customers", p.Audience)
	field("Regions", strings.Join(p.Regions, ", "))
	field("Languages", strings.Join(p.Languages, ", "))
	if p.TargetLeads > 0 {
		field("Lead target", fmt.Sprint(p.TargetLeads))
	}
	section := func(heading string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n## %s\n\n- %s\n", heading, strings.Join(items, "\n- "))
	}
	section("Custom signals", p.Signals)
	section("Sources to read", p.Sources)
	section("Signal words", p.SignalWords)
	section("Bad-actor samples", p.BadActorSamples)
	section("Exclusions", p.Exclusions)
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
	p, err := s.GetProfile(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrNoProfile) {
			logrus.WithContext(ctx).WithError(err).WithField("user_id", userID).Warn("task started without the business profile")
		}
		return input, res
	}
	input = p.Compact() + "\n\n" + strings.TrimSpace(profileBlock.ReplaceAllString(input, ""))
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
