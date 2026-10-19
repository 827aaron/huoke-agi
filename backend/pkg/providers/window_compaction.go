package providers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"pentagi/pkg/csum"
	"pentagi/pkg/providers/pconfig"
	"pentagi/pkg/providers/provider"
	"pentagi/pkg/tools"

	"github.com/vxcontrol/langchaingo/llms"
)

// unknownContextWindow stands in for a model whose window the catalogue does not carry, as a
// custom gateway's seldom does: without it such a chain is never compacted and grows until the
// vendor refuses it.
const unknownContextWindow = 128_000

// ErrContextOverflow marks a chain that is still larger than the model's window after
// compaction and truncation; sending it again can only fail again.
var ErrContextOverflow = errors.New("single context exceeds the limit")

func contextWindowFor(prv provider.Provider, opt pconfig.ProviderOptionsType) int {
	if window := provider.ModelLimitsFor(prv, opt).ContextWindow; window != nil && *window > 0 {
		return *window
	}

	return unknownContextWindow
}

func compactChainForWindow(
	ctx context.Context,
	prv provider.Provider,
	opt pconfig.ProviderOptionsType,
	chain []llms.MessageContent,
	schemas []llms.Tool,
	handler tools.SummarizeHandler,
	tcIDTemplate string,
) ([]llms.MessageContent, bool) {
	window := contextWindowFor(prv, opt)
	if provider.ChainFitsWindow(chain, schemas, window) {
		return chain, false
	}

	budget := provider.ChainByteBudget(window, schemas)
	if budget <= 0 {
		return chain, false
	}

	compacted, err := csum.NewSummarizer(csum.SummarizerConfig{
		PreserveLast:   true,
		UseQA:          true,
		LastSecBytes:   budget / 2,
		MaxBPBytes:     budget / 4,
		MaxQABytes:     budget,
		KeepQASections: 1,
	}).SummarizeChain(ctx, handler, chain, tcIDTemplate)
	if err != nil {
		return chain, false
	}

	return compacted, true
}

// fitChainToWindow is the last step before a chain is sent: a tool answer that compaction could
// not shrink keeps its head and tail, and a chain that still overflows is refused here rather
// than by the vendor after a full upload. A window the tool schemas alone fill is left to the
// provider wrapper, which refuses it without a network call.
func fitChainToWindow(
	prv provider.Provider,
	opt pconfig.ProviderOptionsType,
	chain []llms.MessageContent,
	schemas []llms.Tool,
) ([]llms.MessageContent, bool, error) {
	window := contextWindowFor(prv, opt)
	if provider.ChainFitsWindow(chain, schemas, window) {
		return chain, false, nil
	}

	budget := provider.ChainByteBudget(window, schemas)
	if budget <= 0 {
		return chain, false, nil
	}

	chain, truncated := truncateToolResponses(chain, budget/4)
	if provider.ChainFitsWindow(chain, schemas, window) {
		return chain, truncated, nil
	}

	return chain, truncated, fmt.Errorf(
		"%w: the chain of %s does not fit its %d token context window even after compaction",
		ErrContextOverflow, prv.Model(opt), window,
	)
}

func truncateToolResponses(chain []llms.MessageContent, limit int) ([]llms.MessageContent, bool) {
	fitted, truncated := chain, false
	for mdx, msg := range chain {
		if msg.Role != llms.ChatMessageTypeTool {
			continue
		}

		var parts []llms.ContentPart
		for pdx, part := range msg.Parts {
			response, ok := part.(llms.ToolCallResponse)
			if !ok || len(response.Content) <= limit {
				continue
			}
			if parts == nil {
				parts = slices.Clone(msg.Parts)
			}
			response.Content = csum.TruncateMiddle(response.Content, limit)
			parts[pdx] = response
		}
		if parts == nil {
			continue
		}

		if !truncated {
			fitted, truncated = slices.Clone(chain), true
		}
		fitted[mdx].Parts = parts
	}

	return fitted, truncated
}
