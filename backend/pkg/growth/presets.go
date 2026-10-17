package growth

// ProfilePreset is a starting profile for one kind of customer; the owner copies it into
// the profile form and fills in their own name, regions and languages. Its text is what
// the agents and the judgement model read, so it is English whatever the interface
// language; the interface names a preset by its ID.
type ProfilePreset struct {
	ID      string  `json:"id"`
	Profile Profile `json:"profile"`
}

// ProfilePresets returns fresh copies, so a caller can edit one without touching the
// others.
func ProfilePresets() []ProfilePreset {
	return []ProfilePreset{
		{
			ID: "game_publisher",
			Profile: Profile{
				Pitch:         "A mobile or PC game looking for players who play similar games and are open to a new one.",
				Genre:         "Mobile and PC games",
				DefaultTarget: 50,
				Signals: []string{
					"looking for a new game", "any game like", "bored of", "quitting", "top up", "skins",
					"battle pass", "guide", "build", "rank",
				},
				Excludes: []string{
					"accounts selling or boosting services", "bots and spam", "developers advertising their own game",
				},
				ContactTypes: []string{ContactPlatformHandle},
			},
		},
		{
			ID: "licensed_casino",
			Profile: Profile{
				Pitch: "A licensed online casino and sportsbook looking for adult players who already play online " +
					"and are open to trying a new site.",
				Games:         []string{"online slots", "live casino", "sports betting"},
				Genre:         "Licensed online casino and sportsbook",
				DefaultTarget: 50,
				Signals: []string{
					"deposit", "withdrawal", "payout", "slow to pay", "bonus", "free spins", "cashback", "rebate",
					"VIP", "odds", "which site is legit", "looking for a new site", "recommend a site",
				},
				Excludes: []string{
					"minors or anyone who says they are under 18",
					"people asking for help to stop gambling, self-exclusion or addiction support",
					"people outside the regions the license covers",
					"agents and affiliates advertising other sites", "bots and spam",
				},
				Notes: "Adults only, and only in the regions the license covers; fill those in under Regions. " +
					"Someone who talks about losing control or wanting to quit is not a customer.",
				Scoring: &Scoring{
					Weights:   ScoringWeights{Player: 3, Pain: 3, Paying: 3, Active: 2, Match: 3},
					Qualified: 8,
					Watchlist: WatchlistMinScore,
				},
				ContactTypes: []string{ContactPlatformHandle},
			},
		},
	}
}
