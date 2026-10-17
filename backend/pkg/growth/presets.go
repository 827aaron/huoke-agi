package growth

// ProfilePreset is a starting profile for one kind of customer; the owner copies it into
// the profile form and fills in their own name, regions and languages. Its text is what
// the agents and the judgement model read, so it is English whatever the interface
// language; the interface names a preset by its ID.
type ProfilePreset struct {
	ID string `json:"id"`
	// Default marks the preset the form starts from and leads are judged by while the
	// owner has saved no profile.
	Default bool    `json:"default"`
	Profile Profile `json:"profile"`
}

// ProfilePresets returns fresh copies, so a caller can edit one without touching the
// others.
func ProfilePresets() []ProfilePreset {
	return []ProfilePreset{
		{
			ID:      "licensed_casino",
			Default: true,
			Profile: Profile{
				Pitch: "A licensed, regulated online gaming operator (casino games and sports) serving the regions " +
					"its license covers. Leads are adult players who already discuss online gaming platforms in " +
					"public and say what they want from one; they are delivered to the operator, and nobody is " +
					"contacted on its behalf.",
				Games:         []string{"casino games", "live dealer games", "sports"},
				Genre:         "Licensed online gaming platform",
				DefaultTarget: 50,
				Signals: []string{
					"deposit experience", "withdrawal speed", "bonus terms", "cashback", "VIP program", "odds",
					"platform reliability", "looking for a new platform", "platform recommendation",
				},
				Excludes: []string{
					"a stated age under 18",
					"an explicit request for help to stop gambling, or a stated self-exclusion",
					"a stated location outside the licensed regions",
					"affiliates and agents promoting other platforms", "bots and spam",
				},
				Notes: "The licensed regions are the ones under Regions. Exclusions apply only on explicit evidence in " +
					"the message; never infer them from style, age cues or platform.",
				Scoring: &Scoring{
					Weights:   ScoringWeights{Player: 3, Pain: 3, Paying: 3, Active: 2, Match: 3},
					Qualified: 8,
					Watchlist: WatchlistMinScore,
				},
				ContactTypes: []string{ContactPlatformHandle, ContactWhatsApp, ContactLine, ContactTelegram},
			},
		},
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
	}
}

// DefaultProfile is the profile of the default preset.
func DefaultProfile() Profile {
	for _, p := range ProfilePresets() {
		if p.Default {
			return p.Profile
		}
	}
	return Profile{}
}
