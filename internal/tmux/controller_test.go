package tmux

import (
	"strings"
	"testing"
)

func TestParseRole(t *testing.T) {
	tests := []struct {
		input    string
		expected PaneRole
		wantErr  bool
	}{
		{"top", RoleTop, false},
		{"TOP", RoleTop, false},
		{"Top", RoleTop, false},
		{"left", RoleLeft, false},
		{"LEFT", RoleLeft, false},
		{"right", RoleRight, false},
		{"RIGHT", RoleRight, false},
		{"invalid", "", true},
		{"", "", true},
		{"center", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseRole(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRole(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("ParseRole(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestValidRoles(t *testing.T) {
	roles := ValidRoles()

	if len(roles) != 3 {
		t.Errorf("ValidRoles() returned %d roles, expected 3", len(roles))
	}

	expectedRoles := map[PaneRole]bool{
		RoleTop:   true,
		RoleLeft:  true,
		RoleRight: true,
	}

	for _, role := range roles {
		if !expectedRoles[role] {
			t.Errorf("unexpected role in ValidRoles(): %v", role)
		}
	}
}

func TestRoleToVar(t *testing.T) {
	tests := []struct {
		role     PaneRole
		expected string
	}{
		{RoleTop, VarPaneTop},
		{RoleLeft, VarPaneLeft},
		{RoleRight, VarPaneRight},
		{"invalid", ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			got := roleToVar(tt.role)
			if got != tt.expected {
				t.Errorf("roleToVar(%v) = %q, want %q", tt.role, got, tt.expected)
			}
		})
	}
}

func TestDefaultLayout(t *testing.T) {
	layout := DefaultLayout()

	if layout.TopPercent != 30 {
		t.Errorf("DefaultLayout().TopPercent = %d, want 30", layout.TopPercent)
	}
	if layout.SidePercent != 40 {
		t.Errorf("DefaultLayout().SidePercent = %d, want 40", layout.SidePercent)
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"0", true},
		{"1", true},
		{"123", true},
		{"0000", true},
		{"", false},
		{"abc", false},
		{"12a", false},
		{"1.5", false},
		{"-1", false},
		{" 1", false},
		{"1 ", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isNumeric(tt.input)
			if got != tt.expected {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractLastCommand(t *testing.T) {
	tests := []struct {
		name     string
		capture  string
		expected string
	}{
		{
			name:     "bash prompt with $",
			capture:  "user@host:~$ kubectl get pods",
			expected: "kubectl get pods",
		},
		{
			name:     "zsh prompt with >",
			capture:  "user > ls -la",
			expected: "ls -la",
		},
		{
			name:     "root prompt with #",
			capture:  "root@host:~# cat /etc/hosts",
			expected: "cat /etc/hosts",
		},
		{
			name:     "simple prompt",
			capture:  "$ echo hello",
			expected: "echo hello",
		},
		{
			name: "multiline with command on last line",
			capture: `some output
more output
user@host:~$ git status`,
			expected: "git status",
		},
		{
			name:     "empty capture",
			capture:  "",
			expected: "",
		},
		{
			name:     "no prompt pattern",
			capture:  "just some text",
			expected: "just some text",
		},
		{
			name: "fish prompt",
			capture: `some output
❯ npm install`,
			expected: "npm install",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLastCommand(tt.capture)
			if got != tt.expected {
				t.Errorf("extractLastCommand() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractCommandOutput(t *testing.T) {
	tests := []struct {
		name        string
		fullCapture string
		command     string
		wantContain string // Check if output contains expected content
	}{
		{
			name: "simple output",
			fullCapture: `user@host:~$ echo hello
hello
user@host:~$`,
			command:     "echo hello",
			wantContain: "hello",
		},
		{
			name: "multiline output",
			fullCapture: `user@host:~$ ls
file1.txt
file2.txt
file3.txt
user@host:~$`,
			command:     "ls",
			wantContain: "file1.txt",
		},
		{
			name:        "empty command returns full capture",
			fullCapture: "some output",
			command:     "",
			wantContain: "some output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCommandOutput(tt.fullCapture, tt.command)
			if tt.wantContain != "" && !strings.Contains(got, tt.wantContain) {
				t.Errorf("extractCommandOutput() = %q, want to contain %q", got, tt.wantContain)
			}
		})
	}
}

func TestNewController(t *testing.T) {
	ctrl := NewController()
	if ctrl == nil {
		t.Error("NewController() returned nil")
	}
	if ctrl.sessionName != "" {
		t.Errorf("NewController().sessionName = %q, want empty", ctrl.sessionName)
	}
}

func TestGetSessionName(t *testing.T) {
	ctrl := NewController()
	ctrl.sessionName = "test-session"

	if got := ctrl.GetSessionName(); got != "test-session" {
		t.Errorf("GetSessionName() = %q, want 'test-session'", got)
	}
}

func TestPaneRoleConstants(t *testing.T) {
	// Verify constants have expected values
	if RoleTop != "top" {
		t.Errorf("RoleTop = %q, want 'top'", RoleTop)
	}
	if RoleLeft != "left" {
		t.Errorf("RoleLeft = %q, want 'left'", RoleLeft)
	}
	if RoleRight != "right" {
		t.Errorf("RoleRight = %q, want 'right'", RoleRight)
	}
}

func TestSessionVarConstants(t *testing.T) {
	if VarPaneTop != "@muxctl_top" {
		t.Errorf("VarPaneTop = %q, want '@muxctl_top'", VarPaneTop)
	}
	if VarPaneLeft != "@muxctl_left" {
		t.Errorf("VarPaneLeft = %q, want '@muxctl_left'", VarPaneLeft)
	}
	if VarPaneRight != "@muxctl_right" {
		t.Errorf("VarPaneRight = %q, want '@muxctl_right'", VarPaneRight)
	}
}

func TestShellTypeConstants(t *testing.T) {
	if ShellBash != "bash" {
		t.Errorf("ShellBash = %q, want 'bash'", ShellBash)
	}
	if ShellZsh != "zsh" {
		t.Errorf("ShellZsh = %q, want 'zsh'", ShellZsh)
	}
	if ShellFish != "fish" {
		t.Errorf("ShellFish = %q, want 'fish'", ShellFish)
	}
	if ShellUnknown != "unknown" {
		t.Errorf("ShellUnknown = %q, want 'unknown'", ShellUnknown)
	}
}
