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
	screenBatch      = 40
	screenRunes      = 400
	defaultThreshold = 0.6
)

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

func parseScreening(v map[string]string) (float64, error) {
	raw := strings.TrimSpace(v["threshold"])
	if raw == "" {
		return defaultThreshold, nil
	}
	t, err := strconv.ParseFloat(raw, 64)
	if err != nil || t <= 0 || t >= 1 {
		return 0, fmt.Errorf("%w: threshold must be a number between 0 and 1, like 0.6", ErrInvalidSetting)
	}
	return t, nil
}

type screener struct {
	client    *screening.Client
	threshold float64
	// answeredBy is the model version that last answered, logged when it changes.
	answeredBy string
	now        func() time.Time
}

// screenerFor builds the owner's Jev client.
func (s *Service) screenerFor(ctx context.Context, userID int64) (*screener, error) {
	cfg, err := s.ConfigFor(ctx, userID)
	if err != nil {
		logrus.WithContext(ctx).WithError(err).Warn("pre-screen runs on the server's settings")
	}
	if cfg.TypeSafeAPIKey == "" {
		return nil, ErrNoJudge
	}
	threshold := defaultThreshold
	if v, ok := s.savedValues(ctx, userID, SettingScreening); ok {
		if t, err := parseScreening(v); err == nil {
			threshold = t
		}
	}
	return &screener{client: newJevClient(cfg), threshold: threshold, now: time.Now}, nil
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

// savedValues returns a saved setting's fields, or false when none opens.
func (s *Service) savedValues(ctx context.Context, userID int64, kind string) (map[string]string, bool) {
	row, err := s.db.GetUserGrowthSetting(ctx, database.GetUserGrowthSettingParams{UserID: userID, Kind: kind})
	if err != nil {
		return nil, false
	}
	stored, err := s.open(row)
	if err != nil {
		return nil, false
	}
	return stored.Values, true
}

// screen keeps the messages Jev judges likely to come from a customer and scores each
// by the owner's weights, dropping those below the watchlist line. A batch Jev fails on
// is kept whole and unscored, since a missed buyer costs more than scoring a few extra
// messages; a refused key fails the read.
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
	profile, err := s.judgingProfile(ctx, userID)
	if err != nil {
		return nil, err
	}
	scoring := profile.ScoringOrDefault()
	business := businessSummary(profile)
	kept := make([]MessageView, 0, len(msgs))
	for start := 0; start < len(msgs); start += screenBatch {
		batch := msgs[start:min(start+screenBatch, len(msgs))]
		judged, model, err := sc.client.Judge(ctx, screening.Batch{
			Platform: platform, Source: source, Business: business, Messages: screenMessages(batch),
			Signals: screening.Signals,
		})
		switch {
		case errors.Is(err, screening.ErrUnauthorized):
			return nil, fmt.Errorf("%w (%v)", ErrNoJudge, err)
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case err != nil:
			logrus.WithContext(ctx).WithError(err).WithField("source_id", sourceID).
				Warn("pre-screen failed; the batch goes to scoring unscreened")
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
			if j.Customer >= 0 && j.Customer < sc.threshold {
				continue
			}
			if len(j.Signals) > 0 {
				m.Score, m.Signals = scoreMessage(scoring, j.Signals, m.Time, sc.now())
				if m.Score < scoring.Watchlist {
					continue
				}
				m.Tier = scoring.tier(m.Score)
				if err := s.db.UpsertMessageScore(ctx, database.UpsertMessageScoreParams{
					UserID: userID, SourceID: sourceID, MessageID: m.MessageID, Score: int32(m.Score), Signals: m.Signals,
				}); err != nil {
					return nil, fmt.Errorf("failed to store the score of message %s: %w", m.MessageID, err)
				}
			}
			kept = append(kept, m)
		}
	}
	return kept, nil
}

// judgeEvidence screens the excerpts of a lead whose messages no read scored, as on web
// pages the researcher reads itself: the best excerpt's score, or false when Jev failed
// and the agent's score has to stand. A score is stored per message id, so a repeat of
// the lead is not judged again.
func (s *Service) judgeEvidence(
	ctx context.Context,
	userID int64,
	sc *screener,
	src database.GrowthSource,
	in LeadInput,
) (int, bool, error) {
	profile, err := s.judgingProfile(ctx, userID)
	if err != nil {
		return 0, false, err
	}
	scoring := profile.ScoringOrDefault()
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
	judged, _, err := sc.client.Judge(ctx, screening.Batch{
		Platform: src.Platform, Source: source, Business: businessSummary(profile), Messages: msgs,
		Signals: screening.Signals,
	})
	switch {
	case errors.Is(err, screening.ErrUnauthorized):
		return 0, false, fmt.Errorf("%w (%v)", ErrNoJudge, err)
	case ctx.Err() != nil:
		return 0, false, ctx.Err()
	case err != nil:
		logrus.WithContext(ctx).WithError(err).WithField("source_id", src.ID).
			Warn("judging a lead's evidence failed; the agent's score stands")
		return 0, false, nil
	}

	best, scored := 0, false
	for i, ev := range in.Evidence {
		j, ok := judged[strconv.Itoa(i)]
		if !ok {
			continue
		}
		scored = true
		if j.Customer >= 0 && j.Customer < sc.threshold {
			continue
		}
		score, signals := scoreMessage(scoring, j.Signals, evidenceTime(ev), sc.now())
		best = max(best, score)
		if id := strings.TrimSpace(ev.MessageID); id != "" && len(j.Signals) > 0 {
			if err := s.db.UpsertMessageScore(ctx, database.UpsertMessageScoreParams{
				UserID: userID, SourceID: ev.SourceID, MessageID: id, Score: int32(score), Signals: signals,
			}); err != nil {
				return 0, false, fmt.Errorf("failed to store the score of message %s: %w", id, err)
			}
		}
	}
	return best, scored, nil
}

func evidenceTime(ev EvidenceInput) time.Time {
	if ev.MessageTime != nil {
		return *ev.MessageTime
	}
	return time.Time{}
}

// signalYes is the probability from which Jev's answer counts as yes.
const signalYes = 0.5

// scoreMessage adds the weight of every signal Jev answered yes to, and the active
// weight for a recent message; the text lists what fired, like "player+3, 7d+2 = 5".
func scoreMessage(sc Scoring, signals map[string]float64, at, now time.Time) (int, string) {
	w := sc.Weights
	var (
		score int
		fired []string
	)
	add := func(label string, weight int) {
		if weight > 0 {
			score += weight
			fired = append(fired, fmt.Sprintf("%s+%d", label, weight))
		}
	}
	for _, sig := range []struct {
		key    string
		weight int
	}{
		{screening.SignalPlayer, w.Player}, {screening.SignalPain, w.Pain},
		{screening.SignalPaying, w.Paying}, {screening.SignalMatch, w.Match},
	} {
		if signals[sig.key] >= signalYes {
			add(sig.key, sig.weight)
		}
	}
	switch age := now.Sub(at); {
	case at.IsZero():
	case age <= 7*24*time.Hour:
		add("7d", w.Active)
	case age <= 30*24*time.Hour:
		add("30d", w.Active/2)
	}
	if len(fired) == 0 {
		return 0, "no signal"
	}
	return score, fmt.Sprintf("%s = %d", strings.Join(fired, ", "), score)
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

// businessSummary is the profile as Jev reads it; signal words and exclusions are
// hints to its judgment, not a filter.
func businessSummary(p Profile) string {
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
	line("Usually not a customer", strings.Join(p.Excludes, "; "))
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
