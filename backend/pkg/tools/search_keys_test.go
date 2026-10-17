package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/tools/searchers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keyedSearcher answers with its key, or with the error set for that key.
type keyedSearcher struct {
	key   string
	errs  map[string]error
	calls *[]string
}

func (k *keyedSearcher) Engine() database.SearchengineType { return EngineTavily }
func (k *keyedSearcher) IsAvailable() bool                 { return k.key != "" }

func (k *keyedSearcher) Handle(ctx context.Context, _ searchers.Request) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	*k.calls = append(*k.calls, k.key)
	if err := k.errs[k.key]; err != nil {
		return "", err
	}
	return "result from " + k.key, nil
}

func rotatingTavily(t *testing.T, keys string, errs map[string]error) (searchers.Searcher, *[]string) {
	t.Helper()
	t.Cleanup(func() {
		spentKeys.Lock()
		clear(spentKeys.until)
		spentKeys.Unlock()
	})
	calls := &[]string{}
	engine := withKeyRotation(&config.Config{TavilyAPIKey: keys}, EngineTavily, func(c *config.Config) searchers.Searcher {
		return &keyedSearcher{key: c.TavilyAPIKey, errs: errs, calls: calls}
	})
	return engine, calls
}

func TestSearchKeys_WithKeyRotation_MovesToTheNextKeyOnAQuotaError(t *testing.T) {
	engine, calls := rotatingTavily(t, "key-a,\nkey-b", map[string]error{
		"key-a": searchers.Retryable(errors.New("there are requesting too many results"), 0),
	})

	out, err := engine.Handle(context.Background(), searchers.Request{Query: "mmo"})
	require.NoError(t, err)
	assert.Equal(t, "result from key-b", out)

	_, err = engine.Handle(context.Background(), searchers.Request{Query: "mmo"})
	require.NoError(t, err)
	assert.Equal(t, []string{"key-a", "key-b", "key-b"}, *calls, "a spent key is not asked again")
}

func TestSearchKeys_WithKeyRotation_FailsOnlyWhenEveryKeyIsSpent(t *testing.T) {
	engine, calls := rotatingTavily(t, "key-a,key-b", map[string]error{
		"key-a": searchers.Fatal(errors.New("unexpected status code: 432")),
		"key-b": searchers.Retryable(errors.New("there are requesting too many results"), time.Minute),
	})

	_, err := engine.Handle(context.Background(), searchers.Request{Query: "mmo"})
	assert.True(t, searchers.IsRetryable(err), "the last provider error surfaces: %v", err)

	_, err = engine.Handle(context.Background(), searchers.Request{Query: "mmo"})
	assert.True(t, searchers.IsFatal(err))
	assert.ErrorContains(t, err, "every tavily API key has run out of quota")
	assert.Equal(t, []string{"key-a", "key-b"}, *calls)
}

func TestSearchKeys_WithKeyRotation_KeepsOtherErrorsOnTheSameKey(t *testing.T) {
	engine, calls := rotatingTavily(t, "key-a,key-b", map[string]error{
		"key-a": searchers.Fatal(errors.New("API key is wrong")),
	})

	_, err := engine.Handle(context.Background(), searchers.Request{Query: "mmo"})
	assert.ErrorContains(t, err, "API key is wrong")
	assert.Equal(t, []string{"key-a"}, *calls)
}

func TestSearchKeys_WithKeyRotation_BuildsASingleKeyAsIs(t *testing.T) {
	engine, calls := rotatingTavily(t, " key-only ", nil)

	_, isRotation := engine.(*keyRotation)
	assert.False(t, isRotation)
	out, err := engine.Handle(context.Background(), searchers.Request{Query: "mmo"})
	require.NoError(t, err)
	assert.Equal(t, "result from key-only", out)
	assert.Equal(t, []string{"key-only"}, *calls)
}
