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
	return &screener{client: newJevClient(cfg), threshold: threshold}, nil
}

// jevBackoff is the first retry delay after a 429 or 529; a variable so tests run fast.
var jevBackoff = 2 * time.Second

func newJevClient(cfg *config.Config) *screening.Client {
	c := &screening.Client{URL: cfg.TypeSafeAPIURL, Key: cfg.TypeSafeAPIKey, Retries: 3, Backoff: jevBackoff}
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

// screen keeps the messages Jev judges likely to come from a customer. A batch Jev
// fails on is kept whole, since a missed buyer costs more than scoring a few extra
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
	business := s.businessSummary(ctx, userID)
	kept := make([]MessageView, 0, len(msgs))
	for start := 0; start < len(msgs); start += screenBatch {
		batch := msgs[start:min(start+screenBatch, len(msgs))]
		probs, err := sc.client.Judge(ctx, screening.Batch{
			Platform: platform, Source: source, Business: business, Messages: screenMessages(batch),
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
		for _, m := range batch {
			if p, ok := probs[m.MessageID]; !ok || p >= sc.threshold {
				kept = append(kept, m)
			}
		}
	}
	return kept, nil
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
func (s *Service) businessSummary(ctx context.Context, userID int64) string {
	v, err := s.GetProfile(ctx, userID)
	if err != nil {
		return ""
	}
	p := v.Profile
	var b strings.Builder
	line := func(label, value string) {
		if value = strings.Join(strings.Fields(value), " "); value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, value)
		}
	}
	line("Business", p.Name)
	line("Offer", p.Pitch)
	line("Games", strings.Join(p.Games, ", "))
	line("Genre", p.Genre)
	line("Competitors", strings.Join(p.Competitors, ", "))
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
	_, err := c.Judge(ctx, screening.Batch{
		Platform: "telegram", Source: "test", Business: "Offer: a mobile game",
		Messages: []screening.Message{{ID: "1", Sender: "@player", Text: "any good new mobile games?"}},
	})
	if err != nil {
		return SettingStatusInvalid, err.Error()
	}
	return SettingStatusConnected, ""
}
