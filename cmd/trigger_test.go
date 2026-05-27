package cmd

import (
	"testing"
)

func TestTriggerCmdRegistration(t *testing.T) {
	// Verify that the trigger command is registered in RootCmd
	found := false
	for _, child := range RootCmd.Commands() {
		if child.Name() == "trigger" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected command 'trigger' to be registered under RootCmd, but it was not")
	}

	// Verify child commands
	expectedChildren := []string{"lock", "unlock", "notification", "media", "telemetry", "raw"}
	for _, exp := range expectedChildren {
		childFound := false
		for _, child := range TriggerCmd.Commands() {
			if child.Name() == exp {
				childFound = true
				break
			}
		}
		if !childFound {
			t.Errorf("expected child command %q to be registered under TriggerCmd, but it was not", exp)
		}
	}
}
