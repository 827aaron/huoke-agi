package growth

import (
	"database/sql"
	"testing"

	"pentagi/pkg/database"

	"github.com/stretchr/testify/assert"
)

func TestRunner_RoundNumbers_CountsEachFlowFromOneOldestFirst(t *testing.T) {
	flow := func(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: true} }
	runs := []database.GrowthRun{
		{ID: 9, FlowID: flow(2)}, {ID: 7, FlowID: flow(1)}, {ID: 5, FlowID: flow(2)}, {ID: 3, FlowID: flow(1)},
	}
	assert.Equal(t, map[int64]int{3: 1, 7: 2, 5: 1, 9: 2}, RoundNumbers(runs))
}
