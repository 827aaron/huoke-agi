package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pentagi/pkg/docker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPerformers_CapSpecialistResult_LeavesAShortResultUntouched(t *testing.T) {
	fp := newFlowProvider()
	fp.flowID = 4

	got := fp.capSpecialistResult("pentester", 9, "three leads")

	assert.Equal(t, "three leads", got)
	_, err := os.Stat(filepath.Join(docker.FlowWorkDir(fp.cfg.DataDir, fp.flowID), "leads"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestPerformers_CapSpecialistResult_KeepsTheEndsAndWritesTheFullResult(t *testing.T) {
	fp := newFlowProvider()
	fp.flowID = 4
	fp.cfg.DataDir = t.TempDir()
	full := "HEAD-" + strings.Repeat("m", 20*1024) + "-TAIL"

	got := fp.capSpecialistResult("pentester", 9, full)

	assert.Contains(t, got, "HEAD-")
	assert.Contains(t, got, "-TAIL")
	assert.Contains(t, got, "[truncated bytes")
	assert.Less(t, len(got), len(full))
	assert.Contains(t, got, "/work/leads/pentester-9.md")

	stored, err := os.ReadFile(filepath.Join(docker.FlowWorkDir(fp.cfg.DataDir, fp.flowID), "leads", "pentester-9.md"))
	require.NoError(t, err)
	assert.Equal(t, full, string(stored))
}
