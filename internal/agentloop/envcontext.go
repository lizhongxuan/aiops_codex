package agentloop

import (
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strings"
	"time"
)

// EnvironmentContext captures the runtime environment state that is injected
// into each turn's context. Inspired by Codex's environment_context.rs.
type EnvironmentContext struct {
	// Cwd is the current working directory.
	Cwd string `json:"cwd,omitempty"`
	// Shell is the user's shell (e.g., "bash", "zsh").
	Shell string `json:"shell,omitempty"`
	// OS is the operating system.
	OS string `json:"os,omitempty"`
	// Hostname is the machine hostname.
	Hostname string `json:"hostname,omitempty"`
	// Username is the current user.
	Username string `json:"username,omitempty"`
	// CurrentDate is the current date/time.
	CurrentDate string `json:"current_date,omitempty"`
	// Timezone is the local timezone.
	Timezone string `json:"timezone,omitempty"`
	// NetworkAllowed lists allowed network domains (empty = all allowed).
	NetworkAllowed []string `json:"network_allowed,omitempty"`
	// NetworkDenied lists denied network domains.
	NetworkDenied []string `json:"network_denied,omitempty"`
	// Subagents is a summary of active subagents.
	Subagents string `json:"subagents,omitempty"`
	// ActiveHost is the currently selected remote host ID.
	ActiveHost string `json:"active_host,omitempty"`
	// CustomContext holds additional key-value pairs.
	CustomContext map[string]string `json:"custom_context,omitempty"`
}

// DetectEnvironment builds an EnvironmentContext from the current system state.
func DetectEnvironment() EnvironmentContext {
	ec := EnvironmentContext{
		OS:          runtime.GOOS,
		CurrentDate: time.Now().Format("2006-01-02 15:04:05 MST"),
		Timezone:    time.Now().Location().String(),
	}

	if cwd, err := os.Getwd(); err == nil {
		ec.Cwd = cwd
	}
	if hostname, err := os.Hostname(); err == nil {
		ec.Hostname = hostname
	}
	if u, err := user.Current(); err == nil {
		ec.Username = u.Username
	}

	// Detect shell.
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = os.Getenv("COMSPEC")
	}
	if shell != "" {
		// Extract just the shell name.
		parts := strings.Split(shell, "/")
		ec.Shell = parts[len(parts)-1]
	}

	return ec
}

// SerializeToXML converts the environment context to an XML fragment for
// injection into the conversation, matching Codex's format.
func (ec EnvironmentContext) SerializeToXML() string {
	var lines []string
	if ec.Cwd != "" {
		lines = append(lines, fmt.Sprintf("  <cwd>%s</cwd>", ec.Cwd))
	}
	if ec.Shell != "" {
		lines = append(lines, fmt.Sprintf("  <shell>%s</shell>", ec.Shell))
	}
	if ec.OS != "" {
		lines = append(lines, fmt.Sprintf("  <os>%s</os>", ec.OS))
	}
	if ec.Hostname != "" {
		lines = append(lines, fmt.Sprintf("  <hostname>%s</hostname>", ec.Hostname))
	}
	if ec.Username != "" {
		lines = append(lines, fmt.Sprintf("  <username>%s</username>", ec.Username))
	}
	if ec.CurrentDate != "" {
		lines = append(lines, fmt.Sprintf("  <current_date>%s</current_date>", ec.CurrentDate))
	}
	if ec.Timezone != "" {
		lines = append(lines, fmt.Sprintf("  <timezone>%s</timezone>", ec.Timezone))
	}
	if ec.ActiveHost != "" {
		lines = append(lines, fmt.Sprintf("  <active_host>%s</active_host>", ec.ActiveHost))
	}
	if len(ec.NetworkAllowed) > 0 || len(ec.NetworkDenied) > 0 {
		lines = append(lines, "  <network enabled=\"true\">")
		for _, d := range ec.NetworkAllowed {
			lines = append(lines, fmt.Sprintf("    <allowed>%s</allowed>", d))
		}
		for _, d := range ec.NetworkDenied {
			lines = append(lines, fmt.Sprintf("    <denied>%s</denied>", d))
		}
		lines = append(lines, "  </network>")
	}
	if ec.Subagents != "" {
		lines = append(lines, "  <subagents>")
		for _, line := range strings.Split(ec.Subagents, "\n") {
			lines = append(lines, "    "+line)
		}
		lines = append(lines, "  </subagents>")
	}
	for k, v := range ec.CustomContext {
		lines = append(lines, fmt.Sprintf("  <%s>%s</%s>", k, v, k))
	}

	return "<environment_context>\n" + strings.Join(lines, "\n") + "\n</environment_context>"
}

// Diff compares two EnvironmentContexts and returns a new context containing
// only the fields that changed. This minimizes token usage by only injecting
// deltas between turns.
func (ec EnvironmentContext) Diff(prev EnvironmentContext) EnvironmentContext {
	diff := EnvironmentContext{}
	if ec.Cwd != prev.Cwd {
		diff.Cwd = ec.Cwd
	}
	if ec.CurrentDate != prev.CurrentDate {
		diff.CurrentDate = ec.CurrentDate
	}
	if ec.Timezone != prev.Timezone {
		diff.Timezone = ec.Timezone
	}
	if ec.ActiveHost != prev.ActiveHost {
		diff.ActiveHost = ec.ActiveHost
	}
	if ec.Subagents != prev.Subagents {
		diff.Subagents = ec.Subagents
	}
	// Shell, OS, Hostname, Username rarely change — only include on first turn.
	return diff
}

// IsEmpty returns true if the context has no meaningful fields set.
func (ec EnvironmentContext) IsEmpty() bool {
	return ec.Cwd == "" && ec.Shell == "" && ec.OS == "" &&
		ec.Hostname == "" && ec.Username == "" && ec.CurrentDate == "" &&
		ec.ActiveHost == "" && ec.Subagents == ""
}

// ---------- Session integration ----------

// InjectEnvironmentContext appends the environment context as a contextual
// user message at the start of a new turn. On the first turn it includes
// the full context; on subsequent turns it only includes the diff.
func InjectEnvironmentContext(session *Session, current EnvironmentContext) {
	prev := session.lastEnvContext
	session.lastEnvContext = current

	var ctx EnvironmentContext
	if prev.IsEmpty() {
		// First turn: inject full context.
		ctx = current
	} else {
		// Subsequent turns: inject only diff.
		ctx = current.Diff(prev)
	}

	if ctx.IsEmpty() {
		return
	}

	xml := ctx.SerializeToXML()
	session.ContextManager().AppendUser(xml)
}
