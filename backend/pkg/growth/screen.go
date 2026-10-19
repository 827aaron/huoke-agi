package growth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/growth/screening"
	"pentagi/pkg/system"

	"github.com/sirupsen/logrus"
)

// screenBatch is how many messages one Jev request judges; screenRunes cuts each
// message's text, since a buying signal shows in its opening lines.
const (
	screenBatch = 40
	screenRunes = 400
	// unscoredRunes is how much of a message is kept when its batch could not be judged.
	unscoredRunes = 200
)

// retiredScreeningThreshold is a screening field a page may still send; it is dropped,
// since every message Jev does not exclude is scored.
const retiredScreeningThreshold = "threshold"

// ErrNoJudge means the owner has no working key for Jev, the judgement model every read
// is screened with; like the LLM key, only the customer can fix it.
var ErrNoJudge = errors.New("no judgement model (Jev) is configured: ask the user to fill in its key on the settings page")

const (
	screeningTypeSafe   = "typesafe"
	screeningOpenRouter = "openrouter"
	screeningCustom     = "custom"
)

// screeningProviderOf names the provider a TYPESAFE_API_URL from .env points at.
func screeningProviderOf(rawURL string) string {
	switch {
	case rawURL == "":
		return screeningTypeSafe
	case screening.DefaultModelFor(rawURL) == screening.OpenRouterDefaultModel:
		return screeningOpenRouter
	default:
		return screeningCustom
	}
}

type screener struct {
	client *screening.Client
	// answeredBy is the model version that last answered, logged when it changes.
	answeredBy string
	profile    ProfileView
	now        func() time.Time
	// task is what the customer asked of the scope's flow; it defines whom to look for.
	task string
	// fallbacks counts the batches of this read Jev failed on: their messages went to
	// the agent unscreened, which the read's result and the round's progress report.
	fallbacks int
}

// judgeConfig is the owner's settings when they hold a Jev key, else ErrNoJudge: the one
// check every path that would otherwise read unscreened goes through.
func (s *Service) judgeConfig(ctx context.Context, userID int64) (*config.Config, error) {
	cfg, err := s.ConfigFor(ctx, userID)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).Warn("pre-screen runs on the server's settings")
	}
	if cfg.TypeSafeAPIKey == "" {
		return nil, ErrNoJudge
	}
	return cfg, nil
}

// JudgeReady is ErrNoJudge while the owner has no Jev key, so a round is refused before
// it starts rather than after its agents have read.
func (s *Service) JudgeReady(ctx context.Context, userID int64) error {
	_, err := s.judgeConfig(ctx, userID)
	return err
}

// screenerFor builds the owner's Jev client, judging by the profile of scope's flow.
func (s *Service) screenerFor(ctx context.Context, scope Scope) (*screener, error) {
	cfg, err := s.judgeConfig(ctx, scope.UserID)
	if err != nil {
		return nil, err
	}
	profile, err := s.judgingProfile(ctx, scope)
	if err != nil {
		return nil, err
	}
	return &screener{client: newJevClient(cfg), profile: profile, task: s.taskGoal(ctx, scope), now: time.Now}, nil
}

type roundKey struct{ userID, flowID, taskID int64 }

func (sc Scope) roundKey() roundKey {
	k := roundKey{userID: sc.UserID, flowID: sc.FlowID}
	if sc.TaskID != nil {
		k.taskID = *sc.TaskID
	}
	return k
}

func (s *Service) notePrescreenFallbacks(scope Scope, n int) {
	if n == 0 {
		return
	}
	s.fallbackMu.Lock()
	defer s.fallbackMu.Unlock()
	if s.fallbacks == nil {
		s.fallbacks = map[roundKey]int{}
	}
	s.fallbacks[scope.roundKey()] += n
}

// PrescreenFallbacks is how many pre-screen batches Jev failed on in scope's round since
// this process started; outside a task it is the sum over the owner's rounds, of the
// flow when one is given.
func (s *Service) PrescreenFallbacks(scope Scope) int {
	s.fallbackMu.Lock()
	defer s.fallbackMu.Unlock()
	if scope.TaskID != nil {
		return s.fallbacks[scope.roundKey()]
	}
	total := 0
	for k, n := range s.fallbacks {
		if k.userID == scope.UserID && (scope.FlowID == 0 || k.flowID == scope.FlowID) {
			total += n
		}
	}
	return total
}

func prescreenFallbackWarning(n int, span string) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("the pre-screen (Jev) failed on %d batch(es) %s: those messages came back unscreened and unscored, "+
		"so the two-stage judgement did not save tokens on them; check the Jev key and model on the settings page", n, span)
}

// judgeWarning is the progress payload's warning: the missing key, which blocks every
// read, and the round's pre-screen fallbacks, which do not block but cost tokens.
func (s *Service) judgeWarning(ctx context.Context, userID int64, fallbacks int) string {
	var parts []string
	if err := s.JudgeReady(ctx, userID); err != nil {
		parts = append(parts, err.Error())
	}
	if w := prescreenFallbackWarning(fallbacks, "this round"); w != "" {
		parts = append(parts, w)
	}
	return strings.Join(parts, "; ")
}

// taskGoalRunes bounds each instruction: a preset template runs to a page, and its
// opening lines carry the target.
const taskGoalRunes = 500

// taskGoal is the flow's first instruction and, when it differs, its latest one, so a
// follow-up such as "keep going" keeps the original target. Empty without a flow, and
// when the tasks cannot be read: the profile alone then decides.
func (s *Service) taskGoal(ctx context.Context, scope Scope) string {
	if scope.FlowID == 0 {
		return ""
	}
	tasks, err := s.db.GetFlowTasks(ctx, scope.FlowID)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).WithField("flow_id", scope.FlowID).
			Warn("pre-screen without the task's instruction")
		return ""
	}
	var first, latest string
	for _, t := range tasks {
		if in := strings.Join(strings.Fields(t.Input), " "); in != "" {
			if first == "" {
				first = in
			}
			latest = in
		}
	}
	goal := truncateRunes(first, taskGoalRunes)
	if latest != first {
		goal += "\nLatest instruction: " + truncateRunes(latest, taskGoalRunes)
	}
	return goal
}

// jevBackoff is the first retry delay after a 429 or 529; a variable so tests run fast.
var jevBackoff = 2 * time.Second

func newJevClient(cfg *config.Config) *screening.Client {
	c := &screening.Client{URL: cfg.TypeSafeAPIURL, Key: cfg.TypeSafeAPIKey, Model: cfg.TypeSafeModel, Retries: 3, Backoff: jevBackoff}
	if httpClient, err := system.GetHTTPClient(cfg); err == nil {
		c.HTTP = httpClient
	}
	return c
}

// screen drops the messages Jev finds explicit evidence of exclusion in and scores the
// rest by the owner's weights, dropping those below the watchlist line. A batch Jev fails on
// is kept, shortened and marked unscored, since a missed buyer costs more than passing a
// few extra opening lines on; a refused key fails the read.
func (s *Service) screen(
	ctx context.Context,
	userID int64,
	sc *screener,
	sourceID int64,
	platform, source string,
	msgs []MessageView,
) ([]MessageView, error) {
	if len(msgs) == 0 {
		return msgs, nil
	}
	scoring := sc.profile.ScoringOrDefault()
	kept := make([]MessageView, 0, len(msgs))
	for start := 0; start < len(msgs); start += screenBatch {
		batch := msgs[start:min(start+screenBatch, len(msgs))]
		judged, model, err := sc.client.Judge(ctx, sc.batch(platform, source, screenMessages(batch)))
		switch {
		case errors.Is(err, screening.ErrUnauthorized):
			return nil, fmt.Errorf("%w (%v)", ErrNoJudge, err)
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case err != nil:
			sc.fallbacks++
			logrus.WithContext(ctx).WithError(err).WithField("source_id", sourceID).
				Warn("pre-screen failed; the batch is kept unscored and shortened")
			for i := range batch {
				batch[i].Text = truncateRunes(batch[i].Text, unscoredRunes)
				batch[i].Unscored = true
			}
			kept = append(kept, batch...)
			continue
		}
		if model != sc.answeredBy {
			sc.answeredBy = model
			logrus.WithContext(ctx).WithFields(logrus.Fields{
				"source_id": sourceID, "asked": sc.client.ResolvedModel(), "answered": model,
			}).Info("pre-screen model")
		}
		for _, m := range batch {
			j, ok := judged[m.MessageID]
			if !ok {
				kept = append(kept, m)
				continue
			}
			if j.Excluded >= signalYes {
				continue
			}
			if len(j.Signals) > 0 || j.Unwanted >= signalYes {
				m.Score, m.Signals = scoreMessage(scoring, sc.profile.UnwantedPenaltyOrDefault(), j, m.Time, sc.now())
				if m.Score < scoring.Watchlist {
					continue
				}
				m.Tier = scoring.tier(m.Score)
				if err := s.db.UpsertMessageScore(ctx, database.UpsertMessageScoreParams{
					UserID: userID, ProfileID: sc.profile.ID, SourceID: sourceID, MessageID: m.MessageID,
					Score: int32(m.Score), Signals: m.Signals,
				}); err != nil {
					return nil, fmt.Errorf("failed to store the score of message %s: %w", m.MessageID, err)
				}
			}
			kept = append(kept, m)
		}
	}
	return kept, nil
}

// batch is the Jev request for msgs: the profile's dimensions as questions, its unwanted
// list, and the compliance exclusions.
func (sc *screener) batch(platform, source string, msgs []screening.Message) screening.Batch {
	return screening.Batch{
		Platform: platform, Source: source, Task: sc.task, Business: businessSummary(sc.profile.Profile),
		Excluded: ComplianceExcludes, Unwanted: sc.profile.UnwantedLines(), Messages: msgs,
		Signals: signalsFor(sc.profile.ScoringOrDefault()),
	}
}

func signalsFor(sc Scoring) []screening.Signal {
	out := make([]screening.Signal, 0, len(sc.Dimensions))
	for _, d := range sc.Dimensions {
		out = append(out, screening.SignalFor(d.Key, d.Name, d.Description))
	}
	return out
}

// judgeEvidence screens the excerpts of a lead whose messages no read scored, as on web
// pages the researcher reads itself: the best excerpt's score, whether any excerpt showed
// the sender unwanted, and false when Jev failed and the agent's score has to stand. A
// score is stored per message id, so a repeat of the lead is not judged again.
func (s *Service) judgeEvidence(
	ctx context.Context,
	userID int64,
	sc *screener,
	src database.GrowthSource,
	in LeadInput,
) (int, bool, bool, error) {
	scoring := sc.profile.ScoringOrDefault()
	who := strings.TrimSpace(strings.TrimPrefix(in.Username, "@"))
	if who != "" {
		who = "@" + who
	}
	who = strings.TrimSpace(who + " " + in.DisplayName)
	if who == "" {
		who = in.PlatformUserID
	}
	msgs := make([]screening.Message, 0, len(in.Evidence))
	for i, ev := range in.Evidence {
		msgs = append(msgs, screening.Message{
			ID: strconv.Itoa(i), Sender: who, Text: strings.Join(strings.Fields(truncateRunes(ev.Excerpt, screenRunes)), " "),
		})
	}
	source := src.Title
	if source == "" {
		source = src.Link
	}
	judged, _, err := sc.client.Judge(ctx, sc.batch(src.Platform, source, msgs))
	switch {
	case errors.Is(err, screening.ErrUnauthorized):
		return 0, false, false, fmt.Errorf("%w (%v)", ErrNoJudge, err)
	case ctx.Err() != nil:
		return 0, false, false, ctx.Err()
	case err != nil:
		logrus.WithContext(ctx).WithError(err).WithField("source_id", src.ID).
			Warn("judging a lead's evidence failed; the agent's score stands")
		return 0, false, false, nil
	}

	best, unwanted, scored := 0, false, false
	for i, ev := range in.Evidence {
		j, ok := judged[strconv.Itoa(i)]
		if !ok {
			continue
		}
		scored = true
		if j.Excluded >= signalYes {
			continue
		}
		score, signals := scoreMessage(scoring, sc.profile.UnwantedPenaltyOrDefault(), j, evidenceTime(ev), sc.now())
		best = max(best, score)
		unwanted = unwanted || j.Unwanted >= signalYes
		if id := strings.TrimSpace(ev.MessageID); id != "" && (len(j.Signals) > 0 || j.Unwanted >= signalYes) {
			if err := s.db.UpsertMessageScore(ctx, database.UpsertMessageScoreParams{
				UserID: userID, ProfileID: sc.profile.ID, SourceID: ev.SourceID, MessageID: id,
				Score: int32(score), Signals: signals,
			}); err != nil {
				return 0, false, false, fmt.Errorf("failed to store the score of message %s: %w", id, err)
			}
		}
	}
	return best, unwanted, scored, nil
}

func evidenceTime(ev EvidenceInput) time.Time {
	if ev.MessageTime != nil {
		return *ev.MessageTime
	}
	return time.Time{}
}

// signalYes is the probability from which any of Jev's answers counts as yes.
const signalYes = 0.5

// scoreMessage adds the points of every dimension Jev answered yes to and the recent
// points for a fresh message, and takes penalty off when Jev found the sender unwanted,
// never below zero; the text lists what fired, like "player+3, 7d+2, unwanted-3 = 2".
func scoreMessage(sc Scoring, penalty int, j screening.Judgement, at, now time.Time) (int, string) {
	var (
		score int
		fired []string
	)
	add := func(label string, points int) {
		if points != 0 {
			score += points
			fired = append(fired, fmt.Sprintf("%s%+d", label, points))
		}
	}
	for _, d := range sc.Dimensions {
		if j.Signals[d.Key] >= signalYes {
			add(d.Key, d.Points)
		}
	}
	if sc.Recent != nil {
		switch age := now.Sub(at); {
		case at.IsZero():
		case age <= 7*24*time.Hour:
			add("7d", sc.Recent.Points)
		case age <= 30*24*time.Hour:
			add("30d", sc.Recent.Points/2)
		}
	}
	if j.Unwanted >= signalYes {
		add(LeadFlagUnwanted, -penalty)
	}
	if len(fired) == 0 {
		return 0, "no signal"
	}
	return max(score, 0), fmt.Sprintf("%s = %d", strings.Join(fired, ", "), max(score, 0))
}

// signalActive names the recency weight in a score breakdown.
const signalActive = "active"

// ScorePart is one weight a message's score adds up from.
type ScorePart struct {
	Signal string `json:"signal"`
	Points int    `json:"points"`
}

// scoreParts reads back the text scoreMessage stores, reporting either recency label as
// the active signal; the unwanted penalty is a part with negative points.
func scoreParts(text string) []ScorePart {
	text, _, _ = strings.Cut(text, " = ")
	parts := []ScorePart{}
	for _, field := range strings.Split(text, ", ") {
		i := strings.LastIndexAny(field, "+-")
		if i < 0 {
			continue
		}
		label, n := field[:i], field[i:]
		points, err := strconv.Atoi(n)
		if err != nil {
			continue
		}
		if label == "7d" || label == "30d" {
			label = signalActive
		}
		parts = append(parts, ScorePart{Signal: label, Points: points})
	}
	return parts
}

func (sc Scoring) tier(score int) string {
	switch {
	case score >= sc.Qualified:
		return TierQualified
	case score >= sc.Watchlist:
		return TierWatchlist
	default:
		return ""
	}
}

func screenMessages(batch []MessageView) []screening.Message {
	out := make([]screening.Message, 0, len(batch))
	for _, m := range batch {
		who := m.SenderName
		if m.SenderUsername != "" {
			who = strings.TrimSpace("@" + m.SenderUsername + " " + who)
		}
		if who == "" {
			who = m.SenderID
		}
		if m.SenderIsBot {
			who += " (bot)"
		}
		out = append(out, screening.Message{
			ID: m.MessageID, Sender: who, Text: strings.Join(strings.Fields(truncateRunes(m.Text, screenRunes)), " "),
		})
	}
	return out
}

// businessSummary is the profile as Jev reads it; signal words are hints to its
// judgment, not a filter. The exclusions and the unwanted list travel as their own state.
func businessSummary(p Profile) string {
	if strings.TrimSpace(p.Need) != "" || len(p.Sections) > 0 {
		var b strings.Builder
		b.WriteString(strings.TrimSpace(p.Need))
		for _, s := range p.Sections {
			if body := strings.TrimSpace(s.Body); body != "" {
				fmt.Fprintf(&b, "\n%s: %s", s.Title, strings.Join(strings.Fields(body), " "))
			}
		}
		return strings.TrimSpace(b.String())
	}
	var b strings.Builder
	line := func(label, value string) {
		if value = strings.Join(strings.Fields(value), " "); value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, value)
		}
	}
	line("Business", p.Name)
	line("Offer", p.Pitch)
	line("Products", strings.Join(p.Games, ", "))
	line("Category", p.Genre)
	line("Competitors", strings.Join(p.Competitors, ", "))
	line("Regions", strings.Join(p.Regions, ", "))
	line("Languages", strings.Join(p.Languages, ", "))
	line("Words that often mark a likely customer", strings.Join(p.Signals, "; "))
	line("Notes", p.Notes)
	return strings.TrimSpace(b.String())
}

// probeScreening judges one sample message with the key the owner's work would use.
func probeScreening(ctx context.Context, cfg *config.Config) (string, string) {
	if cfg.TypeSafeAPIKey == "" {
		return SettingStatusInvalid, ErrNoJudge.Error()
	}
	c := newJevClient(cfg)
	c.Retries = 0
	_, model, err := c.Judge(ctx, screening.Batch{
		Platform: "telegram", Source: "test", Business: "Offer: a mobile game",
		Messages: []screening.Message{{ID: "1", Sender: "@player", Text: "any good new mobile games?"}},
	})
	if err != nil {
		return SettingStatusInvalid, err.Error()
	}
	if model == "" {
		model = c.ResolvedModel()
	}
	logrus.WithContext(ctx).WithFields(logrus.Fields{"url": c.ResolvedURL(), "asked": c.ResolvedModel(), "answered": model}).
		Info("pre-screen model tested")
	return SettingStatusConnected, "answered by " + model
}
