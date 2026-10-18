//go:build postgres

package growth

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPresets_ProfilePresets_EachSavesAsAProfile(t *testing.T) {
	w := newWorld(t)
	var ids []string
	for _, p := range ProfilePresets() {
		ids = append(ids, p.ID)
		_, err := w.svc.CreateProfile(context.Background(), w.user, ProfileInput{Profile: p.Profile})
		require.NoError(t, err, p.ID)
	}
	assert.Equal(t, []string{"licensed_casino", "game_publisher"}, ids)
	assert.True(t, ProfilePresets()[0].Default)
	assert.False(t, ProfilePresets()[1].Default)

	casino := ProfilePresets()[0].Profile
	assert.NotContains(t, strings.Join(casino.Unwanted, "\n"), "stop gambling", "the system excludes them, not the profile")
	assert.Contains(t, strings.Join(ComplianceExcludes, "\n"), "stop gambling", "people asking for help to quit are no customers")
}
