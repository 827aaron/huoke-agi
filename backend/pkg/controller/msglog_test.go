package controller

import (
	"context"
	"testing"

	"pentagi/pkg/database"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMsglog_PutAgentMsg_RecordsTheAgentAndPutMsgNone(t *testing.T) {
	q := &createFakeQuerier{}
	mlw := NewFlowMsgLogWorker(q, 7, &cascadeFakePublisher{})
	task := int64(3)

	_, err := mlw.PutAgentMsg(context.Background(), database.MsgchainTypeSearcher, database.MsglogTypeSearch, &task, nil, 0, "", "Looking for groups.")
	require.NoError(t, err)
	_, err = mlw.PutMsg(context.Background(), database.MsglogTypeThoughts, &task, nil, 0, "", "Thinking.")
	require.NoError(t, err)

	msgs := q.recordedMessages()
	require.Len(t, msgs, 2)
	assert.Equal(t, database.NullMsgchainType{MsgchainType: database.MsgchainTypeSearcher, Valid: true}, msgs[0].Agent)
	assert.False(t, msgs[1].Agent.Valid)
}
