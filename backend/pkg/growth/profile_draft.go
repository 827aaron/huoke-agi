package growth

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"pentagi/pkg/database"
)

var (
	ErrNoProfileDraft = errors.New("no profile draft for this task")
)

// ProfileDraftView is the profile a flow's assistant drafted and the customer has not
// saved. BaseProfileID is the saved profile the draft changes; nil drafts a new one.
type ProfileDraftView struct {
	FlowID        int64      `json:"flow_id"`
	BaseProfileID *int64     `json:"base_profile_id"`
	Title         string     `json:"title"`
	Profile       Profile    `json:"profile"`
	UpdatedAt     *time.Time `json:"updated_at"`
}

func draftView(row database.GrowthProfileDraft) (ProfileDraftView, error) {
	v := ProfileDraftView{FlowID: row.FlowID, Title: row.Title, UpdatedAt: nullTime(row.UpdatedAt.Time, row.UpdatedAt.Valid)}
	if row.BaseProfileID.Valid {
		v.BaseProfileID = &row.BaseProfileID.Int64
	}
	if err := json.Unmarshal(row.Profile, &v.Profile); err != nil {
		return ProfileDraftView{}, fmt.Errorf("%w: %w", errCorruptProfile, err)
	}
	v.Profile.normalize()
	return v, nil
}

// SaveProfileDraft replaces the draft of scope's flow; it is checked as a save would be,
// so the customer is never shown a draft that cannot be saved.
func (s *Service) SaveProfileDraft(ctx context.Context, scope Scope, title string, baseID *int64, p Profile) (ProfileDraftView, error) {
	if scope.FlowID == 0 {
		return ProfileDraftView{}, fmt.Errorf("%w: a draft belongs to a task", ErrFlowNotFound)
	}
	in := ProfileInput{Title: title, Profile: p}
	raw, err := in.prepare()
	if err != nil {
		return ProfileDraftView{}, err
	}
	params := database.UpsertGrowthProfileDraftParams{FlowID: scope.FlowID, UserID: scope.UserID, Title: in.Title, Profile: raw}
	if baseID != nil && *baseID != 0 {
		if _, err := s.GetProfile(ctx, scope.UserID, *baseID); err != nil {
			return ProfileDraftView{}, err
		}
		params.BaseProfileID = sql.NullInt64{Int64: *baseID, Valid: true}
	}
	row, err := s.db.UpsertGrowthProfileDraft(ctx, params)
	if err != nil {
		return ProfileDraftView{}, fmt.Errorf("failed to store the profile draft: %w", err)
	}
	return draftView(row)
}

func (s *Service) ProfileDraft(ctx context.Context, userID, flowID int64) (ProfileDraftView, error) {
	row, err := s.db.GetGrowthProfileDraft(ctx, database.GetGrowthProfileDraftParams{FlowID: flowID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return ProfileDraftView{}, ErrNoProfileDraft
	}
	if err != nil {
		return ProfileDraftView{}, fmt.Errorf("failed to load the profile draft: %w", err)
	}
	return draftView(row)
}

func (s *Service) DiscardProfileDraft(ctx context.Context, userID, flowID int64) error {
	n, err := s.db.DeleteGrowthProfileDraft(ctx, database.DeleteGrowthProfileDraftParams{FlowID: flowID, UserID: userID})
	if err != nil {
		return fmt.Errorf("failed to discard the profile draft: %w", err)
	}
	if n == 0 {
		return ErrNoProfileDraft
	}
	return nil
}

// ApplyDraftOptions say how a draft is saved. Title, when set, replaces the draft's;
// AsNew saves a new profile even when the draft changes a saved one; UseForFlow makes the
// draft's flow use the saved profile.
type ApplyDraftOptions struct {
	Title       string `json:"title,omitempty"`
	AsNew       bool   `json:"as_new"`
	UseForFlow  bool   `json:"use_for_flow"`
	MakeDefault bool   `json:"make_default"`
}

// ApplyProfileDraft saves the flow's draft as a profile and discards the draft.
func (s *Service) ApplyProfileDraft(ctx context.Context, userID, flowID int64, opt ApplyDraftOptions) (ProfileView, error) {
	d, err := s.ProfileDraft(ctx, userID, flowID)
	if err != nil {
		return ProfileView{}, err
	}
	in := ProfileInput{Title: cmp.Or(strings.TrimSpace(opt.Title), d.Title), Profile: d.Profile}
	var v ProfileView
	if d.BaseProfileID != nil && !opt.AsNew {
		v, err = s.UpdateProfile(ctx, userID, *d.BaseProfileID, in)
	} else {
		v, err = s.CreateProfile(ctx, userID, in)
	}
	if err != nil {
		return ProfileView{}, err
	}
	if opt.MakeDefault && !v.Default {
		if v, err = s.SetDefaultProfile(ctx, userID, v.ID); err != nil {
			return ProfileView{}, err
		}
	}
	if opt.UseForFlow {
		if err := s.BindFlowProfile(ctx, flowID, v); err != nil {
			return ProfileView{}, err
		}
	}
	if err := s.DiscardProfileDraft(ctx, userID, flowID); err != nil && !errors.Is(err, ErrNoProfileDraft) {
		return ProfileView{}, err
	}
	return v, nil
}

// SetFlowProfile makes a flow use one of the owner's profiles from its next judgement and
// its next task on.
func (s *Service) SetFlowProfile(ctx context.Context, userID, flowID, profileID int64) (FlowProfileView, error) {
	if _, err := s.db.GetUserFlow(ctx, database.GetUserFlowParams{ID: flowID, UserID: userID}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FlowProfileView{}, fmt.Errorf("%w: %d", ErrFlowNotFound, flowID)
		}
		return FlowProfileView{}, fmt.Errorf("failed to load task %d: %w", flowID, err)
	}
	v, err := s.GetProfile(ctx, userID, profileID)
	if err != nil {
		return FlowProfileView{}, err
	}
	if err := s.BindFlowProfile(ctx, flowID, v); err != nil {
		return FlowProfileView{}, err
	}
	return s.FlowProfile(ctx, userID, flowID)
}

// ProfileGuide is how a profile is drafted; the assistant's profile tools carry it.
const ProfileGuide = `A profile is three prompts, plus any prompts the user added:
1. need — who to look for: the offer, the audience, regions, languages, signal words, contacts, notes.
2. exclude — who scores lower. One person or group per line. A hit lowers the score and flags the lead; it never
   drops the lead. Never add under-18 or gambling-help rules: the system always excludes those on explicit
   evidence, whatever this prompt says.
3. scoring — the Jev rules, one per line:
   signal = points | name | what counts as yes
   signal = points | what counts as yes
   signal = points
   recent = points          (message age: full points within 7 days, half within 30; not a Jev question)
   qualified >= N           (a lead counts toward the target from this score)
   watchlist >= M           (kept for review; 1 <= M < N, and the points must be able to reach N)
   unwanted = -P            (points taken off for a hit on the exclude prompt; 0 to 20)
   A line starting with # is a comment. Built-in signals are player, pain, paying and match; keep those keys
   when the meaning is the same so Jev keeps its tuned question. points are 0-20.
sections — extra prompts, each with an id, a title and a body, shown to the agents after the three.
To change one saved profile in place, use growth_update_profile (set, append, add_section, remove_section).
Use growth_draft_profile only when the user wants to review a whole new profile before it is saved.
Ask about what is missing one question at a time. When they ask for a change, change only that part.
Write the prompts in the user's language; write the "what counts as yes" part of a scoring line in English.`
