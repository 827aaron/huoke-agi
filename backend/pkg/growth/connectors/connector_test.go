package connectors

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnector_Outcome_Err(t *testing.T) {
	assert.NoError(t, Outcome{Status: StatusOK, ErrorCode: "CHANNEL_PRIVATE"}.Err())
	assert.EqualError(t, Outcome{Status: StatusRateLimited, ErrorCode: "FLOOD_WAIT", Message: "wait"}.Err(), "rate_limited: FLOOD_WAIT: wait")
	assert.EqualError(t, Outcome{Status: StatusInvalid, Message: "query is required"}.Err(), "invalid_argument: query is required")
}

func TestConnector_MembershipResult_JSONIsFlat(t *testing.T) {
	res := MembershipResult{
		Outcome:    Outcome{Status: StatusRateLimited, ErrorCode: "FLOOD_WAIT", RetryAfter: 30},
		Membership: MembershipNotJoined,
	}
	data, err := json.Marshal(res)
	require.NoError(t, err)
	assert.JSONEq(t, `{"status":"rate_limited","error_code":"FLOOD_WAIT","retry_after":30,"membership":"not_joined"}`, string(data))
}
