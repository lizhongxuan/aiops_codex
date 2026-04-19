package server

import (
	"context"
	"fmt"
	"strings"
)

// CapabilityLayer represents the three-tier capability gateway layers.
const (
	CapabilityLayerStructuredRead     = "structured_read"
	CapabilityLayerControlledMutation = "controlled_mutation"
	CapabilityLayerRawShell           = "raw_shell"
)

const (
	hostSummaryToolName            = "host_summary"
	hostProcessTopToolName         = "host_process_top"
	hostServiceStatusToolName      = "host_service_status"
	hostJournalTailToolName        = "host_journal_tail"
	hostFileExistsToolName         = "host_file_exists"
	hostFileReadToolName           = "host_file_read"
	hostFileSearchToolName         = "host_file_search"
	hostNetworkListenersToolName   = "host_network_listeners"
	hostNetworkConnectionsToolName = "host_network_connections"
	hostPackageVersionToolName     = "host_package_version"
	hostNginxStatusToolName        = "host_nginx_status"
	hostMySQLSummaryToolName       = "host_mysql_summary"
	hostRedisSummaryToolName       = "host_redis_summary"
	hostJVMSummaryToolName         = "host_jvm_summary"

	serviceRestartToolName = "service_restart"
	serviceStopToolName    = "service_stop"
	configApplyToolName    = "config_apply"
	packageInstallToolName = "package_install"
	packageUpgradeToolName = "package_upgrade"
)

// structuredReadToolDef holds the metadata for a single structured read tool.
type structuredReadToolDef struct {
	Name        string
	Description string
	// CommandTemplate is a Go format string; %s placeholders are filled from tool arguments.
	CommandTemplate string
	// ArgKeys lists the argument keys expected from the tool call (order matters for template).
	ArgKeys []string
	// ExtraProperties are additional inputSchema properties beyond host and reason.
	ExtraProperties map[string]map[string]any
	// RequiredArgs are the extra required argument keys (host and reason are always required).
	RequiredArgs []string
}

// structuredReadToolRegistry returns the 14 structured read tool definitions.
func structuredReadToolRegistry() []structuredReadToolDef {
	return []structuredReadToolDef{
		{
			Name:            hostSummaryToolName,
			Description:     "Get a quick system summary of the remote host including hostname, uptime, load, memory, and disk usage.",
			CommandTemplate: `hostname && uptime && free -h | head -3 && df -h / | tail -1`,
		},
		{
			Name:            hostProcessTopToolName,
			Description:     "List the top processes by CPU or memory usage on the remote host.",
			CommandTemplate: `ps aux --sort=-%s | head -n %s`,
			ArgKeys:         []string{"sort_by", "limit"},
			ExtraProperties: map[string]map[string]any{
				"sort_by": {"type": "string", "enum": []string{"cpu", "mem"}, "description": "Sort by cpu or mem."},
				"limit":   {"type": "integer", "minimum": 1, "maximum": 50, "description": "Number of top processes to return."},
			},
		},
		{
			Name:            hostServiceStatusToolName,
			Description:     "Check the status of a systemd service on the remote host.",
			CommandTemplate: `systemctl status %s --no-pager -l 2>&1 | head -30`,
			ArgKeys:         []string{"service"},
			ExtraProperties: map[string]map[string]any{
				"service": {"type": "string", "description": "Systemd service name, e.g. nginx, mysql, redis."},
			},
			RequiredArgs: []string{"service"},
		},
		{
			Name:            hostJournalTailToolName,
			Description:     "Tail recent journal logs for a systemd unit on the remote host.",
			CommandTemplate: `journalctl -u %s --no-pager -n %s --output=short-iso`,
			ArgKeys:         []string{"unit", "lines"},
			ExtraProperties: map[string]map[string]any{
				"unit":  {"type": "string", "description": "Systemd unit name to tail logs for."},
				"lines": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Number of recent log lines."},
			},
			RequiredArgs: []string{"unit"},
		},
		{
			Name:            hostFileExistsToolName,
			Description:     "Check whether a file or directory exists on the remote host.",
			CommandTemplate: `test -e %s && echo "EXISTS" || echo "NOT_FOUND"`,
			ArgKeys:         []string{"path"},
			ExtraProperties: map[string]map[string]any{
				"path": {"type": "string", "description": "Absolute path to check."},
			},
			RequiredArgs: []string{"path"},
		},
		{
			Name:            hostFileReadToolName,
			Description:     toolPromptDescription(hostFileReadToolName),
			CommandTemplate: `head -n %s %s`,
			ArgKeys:         []string{"max_lines", "path"},
			ExtraProperties: map[string]map[string]any{
				"path":      {"type": "string", "description": "Absolute file path to read."},
				"max_lines": {"type": "integer", "minimum": 1, "maximum": 500, "description": "Maximum lines to read."},
			},
			RequiredArgs: []string{"path"},
		},
		{
			Name:            hostFileSearchToolName,
			Description:     toolPromptDescription(hostFileSearchToolName),
			CommandTemplate: `grep -rn --include='*' %s %s | head -n %s`,
			ArgKeys:         []string{"pattern", "path", "max_matches"},
			ExtraProperties: map[string]map[string]any{
				"path":        {"type": "string", "description": "Directory path to search in."},
				"pattern":     {"type": "string", "description": "Text pattern to search for."},
				"max_matches": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Maximum matches to return."},
			},
			RequiredArgs: []string{"path", "pattern"},
		},
		{
			Name:            hostNetworkListenersToolName,
			Description:     "List listening TCP/UDP ports on the remote host.",
			CommandTemplate: `ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null`,
		},
		{
			Name:            hostNetworkConnectionsToolName,
			Description:     "Show active network connections on the remote host, optionally filtered by port.",
			CommandTemplate: `ss -tnp state established 2>/dev/null | head -n 50`,
		},
		{
			Name:            hostPackageVersionToolName,
			Description:     "Check the installed version of a package on the remote host.",
			CommandTemplate: `(dpkg -l %s 2>/dev/null || rpm -q %s 2>/dev/null) | tail -3`,
			ArgKeys:         []string{"package", "package"},
			ExtraProperties: map[string]map[string]any{
				"package": {"type": "string", "description": "Package name to check."},
			},
			RequiredArgs: []string{"package"},
		},
		{
			Name:            hostNginxStatusToolName,
			Description:     "Get Nginx status including version, config test, and active connections on the remote host.",
			CommandTemplate: `nginx -v 2>&1; nginx -t 2>&1; curl -s http://127.0.0.1/nginx_status 2>/dev/null || echo "stub_status not available"`,
		},
		{
			Name:            hostMySQLSummaryToolName,
			Description:     "Get a MySQL/MariaDB summary including version, uptime, and connection count on the remote host.",
			CommandTemplate: `mysqladmin -u root status 2>/dev/null || mysql -u root -e "SHOW GLOBAL STATUS LIKE 'Uptime'; SELECT COUNT(*) AS connections FROM information_schema.processlist;" 2>/dev/null || echo "mysql not accessible"`,
		},
		{
			Name:            hostRedisSummaryToolName,
			Description:     "Get a Redis summary including version, memory, and connected clients on the remote host.",
			CommandTemplate: `redis-cli info server 2>/dev/null | grep -E "redis_version|uptime_in_seconds|connected_clients|used_memory_human" || echo "redis-cli not available"`,
		},
		{
			Name:            hostJVMSummaryToolName,
			Description:     "List running JVM processes and their basic info on the remote host.",
			CommandTemplate: `jps -lv 2>/dev/null || ps aux | grep '[j]ava' | head -10`,
		},
	}
}

// controlledMutationToolDef holds the metadata for a single controlled mutation tool.
// These tools always require approval before execution.
type controlledMutationToolDef struct {
	Name        string
	Description string
	// CommandTemplate is a Go format string; %s placeholders are filled from tool arguments.
	CommandTemplate string
	// ArgKeys lists the argument keys expected from the tool call (order matters for template).
	ArgKeys []string
	// ExtraProperties are additional inputSchema properties beyond host and reason.
	ExtraProperties map[string]map[string]any
	// RequiredArgs are the extra required argument keys (host and reason are always required).
	RequiredArgs []string
}

// controlledMutationToolRegistry returns the controlled mutation tool definitions.
// These tools map to predefined system-changing commands and always require approval.
func controlledMutationToolRegistry() []controlledMutationToolDef {
	return []controlledMutationToolDef{
		{
			Name:            serviceRestartToolName,
			Description:     "Restart a systemd service on the remote host. Always requires approval.",
			CommandTemplate: `systemctl restart %s`,
			ArgKeys:         []string{"service"},
			ExtraProperties: map[string]map[string]any{
				"service": {"type": "string", "description": "Systemd service name to restart, e.g. nginx, mysql, redis."},
			},
			RequiredArgs: []string{"service"},
		},
		{
			Name:            serviceStopToolName,
			Description:     "Stop a systemd service on the remote host. Always requires approval.",
			CommandTemplate: `systemctl stop %s`,
			ArgKeys:         []string{"service"},
			ExtraProperties: map[string]map[string]any{
				"service": {"type": "string", "description": "Systemd service name to stop."},
			},
			RequiredArgs: []string{"service"},
		},
		{
			Name:            configApplyToolName,
			Description:     "Apply a configuration file change on the remote host by writing content to a path. Always requires approval.",
			CommandTemplate: `cat > %s << 'AIOPS_EOF'\n%s\nAIOPS_EOF`,
			ArgKeys:         []string{"path", "content"},
			ExtraProperties: map[string]map[string]any{
				"path":    {"type": "string", "description": "Absolute path of the configuration file to write."},
				"content": {"type": "string", "description": "Full content to write to the configuration file."},
			},
			RequiredArgs: []string{"path", "content"},
		},
		{
			Name:            packageInstallToolName,
			Description:     "Install a package on the remote host using the system package manager. Always requires approval.",
			CommandTemplate: `(command -v apt-get >/dev/null 2>&1 && apt-get install -y %s) || (command -v yum >/dev/null 2>&1 && yum install -y %s) || (command -v dnf >/dev/null 2>&1 && dnf install -y %s) || echo "no supported package manager found"`,
			ArgKeys:         []string{"package", "package", "package"},
			ExtraProperties: map[string]map[string]any{
				"package": {"type": "string", "description": "Package name to install."},
			},
			RequiredArgs: []string{"package"},
		},
		{
			Name:            packageUpgradeToolName,
			Description:     "Upgrade a package on the remote host using the system package manager. Always requires approval.",
			CommandTemplate: `(command -v apt-get >/dev/null 2>&1 && apt-get install --only-upgrade -y %s) || (command -v yum >/dev/null 2>&1 && yum update -y %s) || (command -v dnf >/dev/null 2>&1 && dnf upgrade -y %s) || echo "no supported package manager found"`,
			ArgKeys:         []string{"package", "package", "package"},
			ExtraProperties: map[string]map[string]any{
				"package": {"type": "string", "description": "Package name to upgrade."},
			},
			RequiredArgs: []string{"package"},
		},
	}
}

// controlledMutationToolNames returns the set of all controlled mutation tool names for fast lookup.
func controlledMutationToolNames() map[string]bool {
	registry := controlledMutationToolRegistry()
	names := make(map[string]bool, len(registry))
	for _, def := range registry {
		names[def.Name] = true
	}
	return names
}

func normalizeControlledMutationToolName(name string) string {
	return strings.ReplaceAll(strings.TrimSpace(name), ".", "_")
}

// isControlledMutationTool returns true if the tool name is a controlled mutation tool
// (service.*, config.*, package.* prefix).
func isControlledMutationTool(name string) bool {
	return controlledMutationToolNames()[normalizeControlledMutationToolName(name)]
}

// controlledMutationToolDefinitions returns the controlled mutation tools in the same
// map[string]any format used by remoteDynamicTools().
func controlledMutationToolDefinitions() []map[string]any {
	registry := controlledMutationToolRegistry()
	tools := make([]map[string]any, 0, len(registry))
	for _, def := range registry {
		properties := map[string]any{
			"host": map[string]any{
				"type":        "string",
				"description": "Required selected remote host ID. Must exactly match the current selected host.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Short explanation of why this mutation is needed.",
			},
		}
		required := []string{"host", "reason"}
		for key, schema := range def.ExtraProperties {
			properties[key] = schema
		}
		for _, rk := range def.RequiredArgs {
			required = append(required, rk)
		}
		tools = append(tools, map[string]any{
			"name":        def.Name,
			"description": def.Description,
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           properties,
				"required":             required,
				"additionalProperties": false,
			},
		})
	}
	return tools
}

// buildControlledMutationCommand constructs the shell command for a controlled mutation tool call.
func buildControlledMutationCommand(toolName string, arguments map[string]any) (string, error) {
	toolName = normalizeControlledMutationToolName(toolName)
	registry := controlledMutationToolRegistry()
	var def *controlledMutationToolDef
	for i := range registry {
		if registry[i].Name == toolName {
			def = &registry[i]
			break
		}
	}
	if def == nil {
		return "", fmt.Errorf("unknown controlled mutation tool: %s", toolName)
	}

	if len(def.ArgKeys) == 0 {
		return def.CommandTemplate, nil
	}

	args := make([]any, 0, len(def.ArgKeys))
	for _, key := range def.ArgKeys {
		val := strings.TrimSpace(getStringAny(arguments, key))
		if val == "" {
			return "", fmt.Errorf("controlled mutation tool %s requires argument %q", toolName, key)
		}
		if err := validateStructuredReadArg(key, val); err != nil {
			return "", err
		}
		args = append(args, val)
	}
	return fmt.Sprintf(def.CommandTemplate, args...), nil
}

// structuredReadToolNames returns the set of all structured read tool names for fast lookup.
func structuredReadToolNames() map[string]bool {
	registry := structuredReadToolRegistry()
	names := make(map[string]bool, len(registry))
	for _, def := range registry {
		names[def.Name] = true
	}
	return names
}

func normalizeStructuredReadToolName(name string) string {
	return strings.ReplaceAll(strings.TrimSpace(name), ".", "_")
}

// isStructuredReadTool returns true if the tool name is a structured read tool (host_* canonical form).
func isStructuredReadTool(name string) bool {
	name = normalizeStructuredReadToolName(name)
	return strings.HasPrefix(name, "host_") && structuredReadToolNames()[name]
}

// structuredReadToolDefinitions returns the 14 structured read tools in the same
// map[string]any format used by remoteDynamicTools().
func structuredReadToolDefinitions() []map[string]any {
	registry := structuredReadToolRegistry()
	tools := make([]map[string]any, 0, len(registry))
	for _, def := range registry {
		properties := map[string]any{
			"host": map[string]any{
				"type":        "string",
				"description": "Required selected remote host ID. Must exactly match the current selected host.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "One-sentence explanation of what you are checking.",
			},
		}
		required := []string{"host", "reason"}
		for key, schema := range def.ExtraProperties {
			properties[key] = schema
		}
		for _, rk := range def.RequiredArgs {
			required = append(required, rk)
		}
		tools = append(tools, map[string]any{
			"name":        def.Name,
			"description": def.Description,
			"inputSchema": map[string]any{
				"type":                 "object",
				"properties":           properties,
				"required":             required,
				"additionalProperties": false,
			},
		})
	}
	return tools
}

// buildStructuredReadCommand constructs the shell command for a structured read tool call.
func buildStructuredReadCommand(toolName string, arguments map[string]any) (string, error) {
	toolName = normalizeStructuredReadToolName(toolName)
	registry := structuredReadToolRegistry()
	var def *structuredReadToolDef
	for i := range registry {
		if registry[i].Name == toolName {
			def = &registry[i]
			break
		}
	}
	if def == nil {
		return "", fmt.Errorf("unknown structured read tool: %s", toolName)
	}

	// No arg keys means the command template is static.
	if len(def.ArgKeys) == 0 {
		return def.CommandTemplate, nil
	}

	args := make([]any, 0, len(def.ArgKeys))
	for _, key := range def.ArgKeys {
		val := strings.TrimSpace(getStringAny(arguments, key))
		if val == "" {
			// Apply sensible defaults for optional numeric args.
			switch key {
			case "sort_by":
				val = "cpu"
			case "limit":
				val = "20"
			case "lines":
				val = "50"
			case "max_lines":
				val = "100"
			case "max_matches":
				val = "50"
			default:
				return "", fmt.Errorf("structured read tool %s requires argument %q", toolName, key)
			}
		}
		// Sanitize: reject shell metacharacters in user-supplied values.
		if err := validateStructuredReadArg(key, val); err != nil {
			return "", err
		}
		args = append(args, val)
	}
	return fmt.Sprintf(def.CommandTemplate, args...), nil
}

// validateStructuredReadArg rejects values containing shell metacharacters.
func validateStructuredReadArg(key, value string) error {
	forbidden := []string{";", "&&", "||", "`", "$(", ">", "<", "\n", "\r"}
	for _, f := range forbidden {
		if strings.Contains(value, f) {
			return fmt.Errorf("argument %q contains forbidden characters", key)
		}
	}
	return nil
}

// executeStructuredReadTool handles the execution of a host.* structured read tool.
// It builds the command, validates the host, checks capability, and delegates to
// the same readonly execution path used by execute_readonly_query.
func (a *App) executeStructuredReadTool(sessionID, hostID, rawID string, params dynamicToolCallParams) {
	command, err := buildStructuredReadCommand(params.Tool, params.Arguments)
	if err != nil {
		_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		return
	}

	args := execToolArgs{
		HostID:     hostID,
		Command:    command,
		Reason:     strings.TrimSpace(getStringAny(params.Arguments, "reason")),
		TimeoutSec: 30,
	}

	a.executeReadonlyDynamicTool(sessionID, hostID, rawID, params, args)
}

// executeControlledMutationTool handles the execution of a controlled mutation tool
// (service.*, config.*, package.*). It builds the command and always creates an
// approval request, forcing the operation through the approval flow.
func (a *App) executeControlledMutationTool(sessionID, hostID, rawID string, params dynamicToolCallParams) {
	command, err := buildControlledMutationCommand(params.Tool, params.Arguments)
	if err != nil {
		_ = a.respondCodex(context.Background(), rawID, toolResponse(err.Error(), false))
		return
	}

	args := execToolArgs{
		HostID:     hostID,
		Command:    command,
		Reason:     strings.TrimSpace(getStringAny(params.Arguments, "reason")),
		TimeoutSec: 120,
	}

	// Controlled mutation tools always go through the approval flow (readonly=false).
	a.requestRemoteCommandApproval(sessionID, hostID, rawID, params, args, false)
}
