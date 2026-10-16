package growth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestService_TierForScore_Thresholds(t *testing.T) {
	for score, want := range map[int]string{13: "qualified", 7: "qualified", 6: "watchlist", 4: "watchlist", 3: "", -1: ""} {
		assert.Equal(t, want, TierForScore(score), "score %d", score)
	}
}
