package agent

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptWarnsAboutUntrustedToolOutput(t *testing.T) {
	a := New("test", "base prompt", "")
	prompt := a.BuildSystemPrompt()
	if !strings.Contains(strings.ToLower(prompt), "untrusted") {
		t.Errorf("system prompt should mention untrusted tool output: %q", prompt[:200])
	}
	if !strings.Contains(prompt, "<tool_result>") {
		t.Errorf("system prompt should reference <tool_result> fencing: %q", prompt[:200])
	}
}

func TestBuildSystemPromptIncludesTrustContractWithSkills(t *testing.T) {
	a := New("test", "base prompt", "- skill: do a thing")
	prompt := a.BuildSystemPrompt()
	if !strings.Contains(prompt, "Tool Output Trust") {
		t.Errorf("system prompt with skills should still include trust contract")
	}
}
