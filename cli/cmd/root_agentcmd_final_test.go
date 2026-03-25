package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestRunAgentCmd_ShowMissingID(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"chronos", "agent", "show"}

	err := runAgentCmd()
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("got %v", err)
	}
}

func TestRunAgentCmd_ChatMissingID(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"chronos", "agent", "chat"}

	err := runAgentCmd()
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("got %v", err)
	}
}
