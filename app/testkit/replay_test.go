package testkit_test

import (
	"path/filepath"
	"testing"

	"personal-workflow-platform/app/testkit"
)

func TestGreetingHistoryReplay(t *testing.T) {
	testkit.Replay(t, filepath.Join("testdata", "greeting_history.json"))
}
