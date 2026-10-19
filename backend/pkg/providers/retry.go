package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"pentagi/pkg/providers/provider"

	"github.com/vxcontrol/langchaingo/llms"
	"github.com/vxcontrol/langchaingo/llms/reasoning"
)

var (
	vendorStatusPattern = regexp.MustCompile(`API returned unexpected status code: (\d{3})`)
	// Gateways in front of a vendor word the status their own way, or put it in the JSON body.
	gatewayStatusPattern = regexp.MustCompile(
		`(?i)(?:status[ _]?code|status|http/\d(?:\.\d)?|http[ _]status|http|error[ _]code|"code")"?\s*[:=]?\s*"?(\d{3})\b`,
	)
)

// A request too large for the model fails the same way however often it is sent; vendors and
// gateways that put no 413 in the message still say it in one of these words.
var oversizedRequestPhrases = []string{
	"context length",
	"context_length",
	"context window",
	"maximum context",
	"too many tokens",
	"request too large",
	"payload too large",
	"request entity too large",
	"prompt is too long",
	"input is too long",
}

var insufficientBalancePhrases = []string{
	"payment required",
	"insufficient balance",
	"insufficient_balance",
	"insufficient_quota",
	"insufficient funds",
	"insufficient credit",
	"credit balance is too low",
	"arrearage",
	"余额不足",
}

var rejectedCredentialPhrases = []string{
	"invalid api key",
	"invalid_api_key",
	"incorrect api key",
}

// ErrProviderBalance marks a call the vendor refused because the account has no money left: no
// retry can succeed until someone tops it up, so the subtask waits for the operator instead.
var ErrProviderBalance = errors.New("the LLM provider reports an insufficient balance; top up the account and resume the flow")

// isDeterministicError reports whether the same request with the same options
// can only fail the same way again.
func isDeterministicError(err error) bool {
	if err == nil {
		return false
	}

	var windowErr *provider.ContextWindowError
	return isLocalRefusal(err) ||
		errors.As(err, &windowErr) ||
		errors.Is(err, ErrContextOverflow) ||
		llms.IsContentFilterError(err) ||
		llms.IsTokenLimitError(err) ||
		llms.IsAuthenticationError(err) ||
		isRejectedRequestStatus(err)
}

func isInsufficientBalance(err error) bool {
	if err == nil {
		return false
	}

	if status, ok := requestStatus(err.Error()); ok && status == http.StatusPaymentRequired {
		return true
	}

	return containsAnyPhrase(err.Error(), insufficientBalancePhrases)
}

// isLocalRefusal reports whether the library decided the request cannot succeed
// before it reached the network.
func isLocalRefusal(err error) bool {
	var (
		structuredUnsupported *llms.ErrStructuredOutputUnsupported
		structuredConflict    *llms.ErrStructuredOutputConflict
		reasoningOff          *reasoning.ErrReasoningOffUnsupported
		forcedToolUse         *reasoning.ErrForcedToolUseWithThinking
		assistantPrefill      *reasoning.ErrAssistantPrefillUnsupported
		effortHasNoBudget     *reasoning.ErrEffortHasNoBudget
		effortWithTools       *reasoning.ErrEffortWithTools
		budgetUnsupported     *reasoning.ErrThinkingBudgetUnsupported
		thinkingNeedsStream   *reasoning.ErrThinkingRequiresStream
	)

	return errors.As(err, &structuredUnsupported) ||
		errors.As(err, &structuredConflict) ||
		errors.As(err, &reasoningOff) ||
		errors.As(err, &forcedToolUse) ||
		errors.As(err, &assistantPrefill) ||
		errors.As(err, &effortHasNoBudget) ||
		errors.As(err, &effortWithTools) ||
		errors.As(err, &budgetUnsupported) ||
		errors.As(err, &thinkingNeedsStream) ||
		errors.Is(err, llms.ErrStructuredOutputConfig)
}

// A 408 or a 429 is transient even when its message also names tokens: a per-minute token
// quota reads like an oversized request.
func isRejectedRequestStatus(err error) bool {
	message := err.Error()
	status, ok := requestStatus(message)
	switch {
	case ok && (status == http.StatusRequestTimeout || status == http.StatusTooManyRequests):
		return false
	case ok && status >= 400 && status < 500:
		return true
	}

	return containsAnyPhrase(message, oversizedRequestPhrases) ||
		containsAnyPhrase(message, insufficientBalancePhrases) ||
		containsAnyPhrase(message, rejectedCredentialPhrases)
}

// requestStatus reads the first HTTP status the message carries; the library's own wording wins
// over a number a gateway put further along.
func requestStatus(message string) (int, bool) {
	match := vendorStatusPattern.FindStringSubmatch(message)
	if match == nil {
		match = gatewayStatusPattern.FindStringSubmatch(message)
	}
	if match == nil {
		return 0, false
	}

	status, err := strconv.Atoi(match[1])
	if err != nil || status < 100 || status > 599 {
		return 0, false
	}

	return status, true
}

func containsAnyPhrase(message string, phrases []string) bool {
	lower := strings.ToLower(message)
	for _, phrase := range phrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}

	return false
}

func retryTransient[T any](ctx context.Context, delay time.Duration, call func() (T, error)) (T, error) {
	var zero T
	for attempt := 1; ; attempt++ {
		result, err := call()
		switch {
		case err == nil:
			return result, nil
		case errors.Is(err, context.Canceled) || isDeterministicError(err):
			return zero, err
		case attempt == maxRetriesToCallSimpleChain:
			return zero, fmt.Errorf("gave up after %d attempts: %w", attempt, err)
		}

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
	}
}
