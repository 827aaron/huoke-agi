package growth

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"
)

// TelegramPace is the rhythm suggested for Telegram accounts, the way a new account is
// warmed up so Telegram's anti-spam leaves it alone. The join values only raise reminders;
// the read interval and the first-sync window shape reads. The customer changes every
// value in the telegram setting.
type TelegramPace struct {
	// Ramp[d] is the most joins in 24 hours on day d+1 after the account's first sign-in;
	// PerDay applies once the ramp is over.
	Ramp   []int
	PerDay int
	// The gap after a join is random within [IntervalMin, IntervalMax].
	IntervalMin time.Duration
	IntervalMax time.Duration
	// PrivatePerDay counts only joins through an invite link.
	PrivatePerDay int
	// An account's reads of two sources are a random [ReadIntervalMin, ReadIntervalMax] apart.
	ReadIntervalMin time.Duration
	ReadIntervalMax time.Duration
	// WarnAt is the number of joined groups at which the sources page reminds; Telegram
	// refuses joins past about 500.
	WarnAt int
	// A source's backward reading stops FirstSyncDays before the account first read it,
	// or after FirstSyncMessages messages, whichever comes first.
	FirstSyncDays     int
	FirstSyncMessages int
	// Reminders turns the join advice and the joined-groups warning on; the values above
	// are suggestions and never stop a join.
	Reminders bool
}

var defaultTelegramPace = TelegramPace{
	Ramp:              []int{0, 0, 0, 1, 1, 2, 2, 3, 3, 3, 3, 3, 3, 3},
	PerDay:            5,
	IntervalMin:       3 * time.Minute,
	IntervalMax:       10 * time.Minute,
	PrivatePerDay:     2,
	ReadIntervalMin:   5 * time.Second,
	ReadIntervalMax:   15 * time.Second,
	WarnAt:            450,
	FirstSyncDays:     7,
	FirstSyncMessages: 300,
	Reminders:         true,
}

// Telegram setting fields of the pace; empty takes the default.
const (
	paceFieldRamp          = "join_ramp"
	paceFieldPerDay        = "joins_per_day"
	paceFieldInterval      = "join_interval_minutes"
	paceFieldPrivatePerDay = "private_joins_per_day"
	paceFieldReadInterval  = "read_interval_seconds"
	paceFieldWarnAt        = "joined_groups_warning"
	paceFieldSyncDays      = "first_sync_days"
	paceFieldSyncMessages  = "first_sync_messages"
	paceFieldReminders     = "risk_reminders"
)

func joinList(items []int) string {
	parts := make([]string, len(items))
	for i, n := range items {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

var paceFields = []settingField{
	{name: paceFieldRamp, def: joinList(defaultTelegramPace.Ramp)},
	{name: paceFieldPerDay, def: strconv.Itoa(defaultTelegramPace.PerDay)},
	{name: paceFieldInterval, def: fmt.Sprintf("%d-%d",
		int(defaultTelegramPace.IntervalMin/time.Minute), int(defaultTelegramPace.IntervalMax/time.Minute))},
	{name: paceFieldPrivatePerDay, def: strconv.Itoa(defaultTelegramPace.PrivatePerDay)},
	{name: paceFieldReadInterval, def: fmt.Sprintf("%d-%d",
		int(defaultTelegramPace.ReadIntervalMin/time.Second), int(defaultTelegramPace.ReadIntervalMax/time.Second))},
	{name: paceFieldWarnAt, def: strconv.Itoa(defaultTelegramPace.WarnAt)},
	{name: paceFieldSyncDays, def: strconv.Itoa(defaultTelegramPace.FirstSyncDays)},
	{name: paceFieldSyncMessages, def: strconv.Itoa(defaultTelegramPace.FirstSyncMessages)},
	{name: paceFieldReminders, def: "on"},
}

func parseTelegramPace(v map[string]string) (TelegramPace, error) {
	p := defaultTelegramPace
	count := func(field string, dst *int) error {
		raw := strings.TrimSpace(v[field])
		if raw == "" {
			return nil
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return fmt.Errorf("%w: %s must be a whole number, 0 or more", ErrInvalidSetting, field)
		}
		*dst = n
		return nil
	}
	for field, dst := range map[string]*int{
		paceFieldPerDay:        &p.PerDay,
		paceFieldPrivatePerDay: &p.PrivatePerDay,
		paceFieldWarnAt:        &p.WarnAt,
		paceFieldSyncDays:      &p.FirstSyncDays,
		paceFieldSyncMessages:  &p.FirstSyncMessages,
	} {
		if err := count(field, dst); err != nil {
			return TelegramPace{}, err
		}
	}

	if raw := strings.TrimSpace(v[paceFieldRamp]); raw != "" {
		p.Ramp = nil
		for _, item := range strings.Split(raw, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(item))
			if err != nil || n < 0 {
				return TelegramPace{}, fmt.Errorf("%w: %s lists the joins allowed on each day from the first sign-in, "+
					"comma separated, such as 0,0,0,1,1,2,2,3", ErrInvalidSetting, paceFieldRamp)
			}
			p.Ramp = append(p.Ramp, n)
		}
	}

	switch strings.ToLower(strings.TrimSpace(v[paceFieldReminders])) {
	case "", "on":
	case "off":
		p.Reminders = false
	default:
		return TelegramPace{}, fmt.Errorf("%w: %s is on or off", ErrInvalidSetting, paceFieldReminders)
	}

	var err error
	if p.IntervalMin, p.IntervalMax, err = parseRange(v[paceFieldInterval], time.Minute, p.IntervalMin, p.IntervalMax); err != nil {
		return TelegramPace{}, fmt.Errorf("%w: %s is minutes between two joins, a number or a range such as 3-10",
			ErrInvalidSetting, paceFieldInterval)
	}
	if p.ReadIntervalMin, p.ReadIntervalMax, err = parseRange(v[paceFieldReadInterval], time.Second,
		p.ReadIntervalMin, p.ReadIntervalMax); err != nil {
		return TelegramPace{}, fmt.Errorf("%w: %s is seconds between reading two sources, a number or a range such as 5-15",
			ErrInvalidSetting, paceFieldReadInterval)
	}
	return p, nil
}

// parseRange reads "n" or "lo-hi" in unit; empty keeps the defaults.
func parseRange(raw string, unit, defLo, defHi time.Duration) (time.Duration, time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defLo, defHi, nil
	}
	lo, hi, found := strings.Cut(raw, "-")
	if !found {
		hi = lo
	}
	nLo, errLo := strconv.Atoi(strings.TrimSpace(lo))
	nHi, errHi := strconv.Atoi(strings.TrimSpace(hi))
	if errLo != nil || errHi != nil || nLo < 0 || nHi < nLo {
		return 0, 0, errors.New("not a range")
	}
	return time.Duration(nLo) * unit, time.Duration(nHi) * unit, nil
}

// telegramPace reads the customer's pace; a missing or unreadable setting gives the defaults.
func (s *Service) telegramPace(ctx context.Context, userID int64) TelegramPace {
	row, err := s.db.GetUserGrowthSetting(ctx, database.GetUserGrowthSettingParams{UserID: userID, Kind: SettingTelegram})
	if err != nil {
		return defaultTelegramPace
	}
	stored, err := s.open(row)
	if err != nil {
		return defaultTelegramPace
	}
	p, err := parseTelegramPace(stored.Values)
	if err != nil {
		return defaultTelegramPace
	}
	return p
}

func (p TelegramPace) perDay(day int) int {
	if day < len(p.Ramp) {
		return p.Ramp[day]
	}
	return p.PerDay
}

// gap is the random pause after a join.
func (p TelegramPace) gap() time.Duration {
	return between(p.IntervalMin, p.IntervalMax)
}

func between(lo, hi time.Duration) time.Duration {
	if hi <= lo {
		return lo
	}
	return lo + rand.N(hi-lo+1)
}

// awaitReadGap holds an account's read of a source until the random gap since its read of
// another source has passed, and claims the slot. Pages of the same source follow each other
// at the connector's own call pace.
func (s *Service) awaitReadGap(ctx context.Context, accountID, sourceID int64, p TelegramPace) error {
	s.readMu.Lock()
	if s.lastRead == nil {
		s.lastRead = make(map[int64]sourceRead)
	}
	last := s.lastRead[accountID]
	at := time.Now()
	if last.sourceID != sourceID {
		if next := last.at.Add(between(p.ReadIntervalMin, p.ReadIntervalMax)); next.After(at) {
			at = next
		}
	}
	s.lastRead[accountID] = sourceRead{sourceID: sourceID, at: at}
	s.readMu.Unlock()

	wait := time.Until(at)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type sourceRead struct {
	sourceID int64
	at       time.Time
}

// joinLoad is what an account has joined lately.
type joinLoad struct {
	signedInAt time.Time
	nextJoinAt time.Time
	joins      int64
	private    int64
}

// Join advice codes: the account goes past a suggested value of its pace. They never stop a join.
const (
	AdviceWarmingUp    = "warming_up"
	AdviceDailyJoins   = "daily_joins_reached"
	AdviceJoinInterval = "join_interval"
	AdvicePrivateJoins = "private_joins_reached"
	AdviceJoinedGroups = "joined_groups_high"
)

var adviceText = map[string]string{
	AdviceWarmingUp:    "the account was connected recently; its suggested warm-up has no joins yet",
	AdviceDailyJoins:   "the account has reached its suggested joins for the last 24 hours",
	AdviceJoinInterval: "the suggested interval since the account's previous join has not passed",
	AdvicePrivateJoins: "the account has reached its suggested invite-link joins for the last 24 hours",
}

// advice lists where one more join would go past the pace's suggestions; empty when it
// fits them or reminders are off.
func (p TelegramPace) advice(now time.Time, l joinLoad, private bool) []string {
	if !p.Reminders {
		return nil
	}
	var codes []string
	day := max(int(now.Sub(l.signedInAt)/(24*time.Hour)), 0)
	switch perDay := int64(p.perDay(day)); {
	case perDay == 0 && day < len(p.Ramp):
		codes = append(codes, AdviceWarmingUp)
	case l.joins >= perDay:
		codes = append(codes, AdviceDailyJoins)
	}
	if l.nextJoinAt.After(now) {
		codes = append(codes, AdviceJoinInterval)
	}
	if private && l.private >= int64(p.PrivatePerDay) {
		codes = append(codes, AdvicePrivateJoins)
	}
	return codes
}

func adviceSentence(codes []string) string {
	parts := make([]string, 0, len(codes))
	for _, c := range codes {
		parts = append(parts, adviceText[c])
	}
	return strings.Join(parts, "; ")
}

// joinIntent carries the pace into account selection for a join; nil means no join.
// pickAccount records the advice of the account it picks.
type joinIntent struct {
	pace    TelegramPace
	private bool
	advice  map[int64][]string
}

func (s *Service) joinIntentFor(ctx context.Context, userID int64, src database.GrowthSource) *joinIntent {
	if src.Platform != string(connectors.PlatformTelegram) {
		return nil
	}
	return &joinIntent{pace: s.telegramPace(ctx, userID), private: isInviteLink(src.Link), advice: map[int64][]string{}}
}

func isInviteLink(link string) bool {
	canonical, err := canonicalTelegramLink(link)
	return err == nil && strings.HasPrefix(canonical, "t.me/+")
}

// JoinWarning names an account that has joined enough groups to approach Telegram's cap; joins go on.
type JoinWarning struct {
	AccountID     int64  `json:"account_id"`
	Platform      string `json:"platform"`
	Label         string `json:"label"`
	JoinedSources int64  `json:"joined_sources"`
	WarnAt        int    `json:"warn_at"`
}

// JoinWarnings lists the customer's Telegram accounts at or past the warning count.
func (s *Service) JoinWarnings(ctx context.Context, userID int64) ([]JoinWarning, error) {
	pace := s.telegramPace(ctx, userID)
	if !pace.Reminders || pace.WarnAt <= 0 {
		return []JoinWarning{}, nil
	}
	counts, err := s.db.GetUserAccountJoinCounts(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to count joined sources: %w", err)
	}
	warnings := []JoinWarning{}
	for _, c := range counts {
		if c.Platform != string(connectors.PlatformTelegram) || c.Joined < int64(pace.WarnAt) {
			continue
		}
		warnings = append(warnings, JoinWarning{
			AccountID: c.AccountID, Platform: c.Platform, Label: c.Label, JoinedSources: c.Joined, WarnAt: pace.WarnAt,
		})
	}
	return warnings, nil
}
