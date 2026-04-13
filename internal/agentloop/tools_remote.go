package agentloop

// RegisterRemoteHostTools registers the remote-host tool definitions into the
// given ToolRegistry. Handlers are placeholder stubs — actual implementations
// are wired during server integration (task 15).
func RegisterRemoteHostTools(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "execute_readonly_query",
		Description: "Run a read-only shell command on the currently selected remote host. Use for inspection only (uptime, df, ps, grep, tail, journalctl, etc.).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Required selected remote host ID.",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Read-only shell command to run.",
				},
				"cwd": map[string]interface{}{
					"type":        "string",
					"description": "Optional working directory on the remote host.",
				},
				"timeout_sec": map[string]interface{}{
					"type":        "integer",
					"minimum":     1,
					"maximum":     120,
					"description": "Optional timeout in seconds.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of what you are checking.",
				},
			},
			"required":             []string{"host", "command", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:             "execute_command",
		Description:      "Run a shell command that changes system state on the currently selected remote host. Always requires user approval.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Required selected remote host ID.",
				},
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command to run after user approval.",
				},
				"cwd": map[string]interface{}{
					"type":        "string",
					"description": "Optional working directory on the remote host.",
				},
				"timeout_sec": map[string]interface{}{
					"type":        "integer",
					"minimum":     1,
					"maximum":     600,
					"description": "Optional timeout in seconds.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Short explanation of why this change is needed.",
				},
			},
			"required":             []string{"host", "command", "reason"},
			"additionalProperties": false,
		},
		RequiresApproval: true,
	})

	reg.Register(ToolEntry{
		Name:        "list_files",
		Description: "List files or directories on the currently selected remote host.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Required selected remote host ID.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Directory path to inspect.",
				},
				"recursive": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to recursively list descendant entries.",
				},
				"max_entries": map[string]interface{}{
					"type":        "integer",
					"minimum":     1,
					"maximum":     500,
					"description": "Maximum number of entries to return.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of what you are inspecting.",
				},
			},
			"required":             []string{"host", "path", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "read_file",
		Description: "Read a file from the currently selected remote host.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Required selected remote host ID.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute or relative file path on the remote host.",
				},
				"max_bytes": map[string]interface{}{
					"type":        "integer",
					"minimum":     256,
					"maximum":     262144,
					"description": "Optional maximum bytes to read.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of what you are checking.",
				},
			},
			"required":             []string{"host", "path", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:        "search_files",
		Description: "Search for text in files on the currently selected remote host.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Required selected remote host ID.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "File or directory path to search.",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Text to search for.",
				},
				"max_matches": map[string]interface{}{
					"type":        "integer",
					"minimum":     1,
					"maximum":     200,
					"description": "Maximum number of matches to return.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "One-sentence explanation of what you are searching for.",
				},
			},
			"required":             []string{"host", "path", "query", "reason"},
			"additionalProperties": false,
		},
		IsReadOnly: true,
	})

	reg.Register(ToolEntry{
		Name:             "write_file",
		Description:      "Write content to a file on the currently selected remote host. Requires user approval.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"host": map[string]interface{}{
					"type":        "string",
					"description": "Required selected remote host ID.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Target file path on the remote host.",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "File content to write.",
				},
				"write_mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"overwrite", "append"},
					"description": "Write mode: overwrite or append.",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Short explanation of why this file change is needed.",
				},
			},
			"required":             []string{"host", "path", "content", "reason"},
			"additionalProperties": false,
		},
		RequiresApproval: true,
	})
}
