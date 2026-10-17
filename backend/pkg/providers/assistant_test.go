package providers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssistant_WithDelegationTimeout_StopsASpecialistThatNeverAnswers(t *testing.T) {
	stuck := func(ctx context.Context, _ string, _ json.RawMessage) (string, error) {
		select {
		case <-ctx.Done():
			return "", errors.New("model request aborted")
		case <-time.After(10 * time.Second):
			return "", errors.New("the delegation was never stopped")
		}
	}

	_, err := withDelegationTimeout(stuck, 50*time.Millisecond)(t.Context(), "pentester", nil)
	require.Error(t, err)
	assert.Equal(t, "no result within 50ms, so it was stopped", err.Error())

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = withDelegationTimeout(stuck, time.Hour)(ctx, "pentester", nil)
	assert.EqualError(t, err, "model request aborted", "the caller's own deadline is not this timeout")

	answered := func(context.Context, string, json.RawMessage) (string, error) { return "3 leads saved", nil }
	got, err := withDelegationTimeout(answered, 0)(t.Context(), "pentester", nil)
	require.NoError(t, err)
	assert.Equal(t, "3 leads saved", got)
}
