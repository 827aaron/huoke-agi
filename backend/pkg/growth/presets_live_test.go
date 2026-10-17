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
		_, err := w.svc.SaveProfile(context.Background(), w.user, p.Profile)
		require.NoError(t, err, p.ID)
	}
	assert.Equal(t, []string{"game_publisher", "licensed_casino"}, ids)

	casino := ProfilePresets()[1].Profile
	assert.Contains(t, strings.Join(casino.Excludes, "\n"), "stop gambling", "people asking for help to quit are no customers")
}
