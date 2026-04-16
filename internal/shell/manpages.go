package shell

// commandManPage holds the content for a command's help/man page.
type commandManPage struct {
	Title       string
	Synopsis    string
	Description []string
	Subcommands []manSubcommand
	Examples    []manExample
	Notes       []string
}

type manSubcommand struct {
	Name string
	Args string
	Desc string
}

type manExample struct {
	Command string
	Desc    string
}

// isHelpRequest returns true if the argument looks like a help flag.
func isHelpRequest(arg string) bool {
	switch arg {
	case "help", "-help", "--help", "-h":
		return true
	default:
		return false
	}
}

func renderManPage(page commandManPage) string {
	lines := []string{divider()}

	if page.Synopsis != "" {
		lines = append(lines, renderManKV("usage", white(page.Synopsis)))
		lines = append(lines, divider())
	}

	if len(page.Description) > 0 {
		for _, line := range page.Description {
			lines = append(lines, dim(line))
		}
		lines = append(lines, divider())
	}

	if len(page.Subcommands) > 0 {
		lines = append(lines, teal("Subcommands:"))
		for _, sub := range page.Subcommands {
			name := teal(padCmd(sub.Name, 14))
			args := ""
			if sub.Args != "" {
				args = purple(sub.Args) + "  "
			}
			lines = append(lines, "  "+name+" "+args+dim(sub.Desc))
		}
		lines = append(lines, divider())
	}

	if len(page.Examples) > 0 {
		lines = append(lines, dim("Examples:"))
		for _, ex := range page.Examples {
			lines = append(lines, "  "+white(ex.Command))
			if ex.Desc != "" {
				lines = append(lines, "    "+dim(ex.Desc))
			}
		}
		lines = append(lines, divider())
	}

	if len(page.Notes) > 0 {
		for _, note := range page.Notes {
			lines = append(lines, dim(note))
		}
		lines = append(lines, divider())
	}

	return renderPanel(page.Title,
		[]panelOption{withSingleBorder(), withMinWidth(62)},
		lines...)
}

func padCmd(s string, width int) string {
	for len(s) < width {
		s += " "
	}
	return s
}

// ── Man pages ────────────────────────────────────────────────────────────────

func manSandbox() commandManPage {
	return commandManPage{
		Title:    "SANDBOX",
		Synopsis: "/sandbox <subcommand> [args...]",
		Description: []string{
			"Manage the content sandbox — import files in, stage",
			"artifacts, inspect metadata, or export to the real filesystem.",
		},
		Subcommands: []manSubcommand{
			{Name: "import", Args: "<src> [dest]", Desc: "import a file into the sandbox"},
			{Name: "stage", Args: "<sandbox-path> [tag]", Desc: "stage an artifact for review"},
			{Name: "metadata", Args: "<sandbox-path>", Desc: "show artifact metadata"},
			{Name: "export", Args: "<sandbox-path> <dest>", Desc: "export from sandbox to filesystem"},
		},
		Examples: []manExample{
			{Command: "/sandbox import ./data.csv input/data.csv"},
			{Command: "/sandbox stage outputs/report.html final-report"},
			{Command: "/sandbox metadata outputs/report.html"},
			{Command: "/sandbox export outputs/report.html ./report.html"},
		},
		Notes: []string{
			"Export is approval-gated. The sandbox is isolated from the",
			"real filesystem — import/export are the only bridges.",
		},
	}
}

func manSite() commandManPage {
	return commandManPage{
		Title:    "SITE",
		Synopsis: "/site <subcommand> <url>",
		Description: []string{
			"Inspect external sites or create trust drafts for",
			"public-read access through Loopgate.",
		},
		Subcommands: []manSubcommand{
			{Name: "inspect", Args: "<url>", Desc: "inspect a site's trust posture"},
			{Name: "trust-draft", Args: "<url> [scope]", Desc: "create a public_read trust draft"},
		},
		Examples: []manExample{
			{Command: "/site inspect https://example.com"},
			{Command: "/site trust-draft https://api.example.com public_read"},
		},
	}
}

func manConnections() commandManPage {
	return commandManPage{
		Title:    "CONNECTIONS",
		Synopsis: "/connections [subcommand] [args...]",
		Description: []string{
			"View and manage Loopgate-managed connections.",
			"Without a subcommand, shows configured connections.",
		},
		Subcommands: []manSubcommand{
			{Name: "validate", Args: "<provider> <subject>", Desc: "validate a connection"},
			{Name: "pkce-start", Args: "<provider> <subject>", Desc: "start a PKCE OAuth flow"},
			{Name: "pkce-complete", Args: "<prov> <subj> <state> <code>", Desc: "complete PKCE exchange"},
		},
		Examples: []manExample{
			{Command: "/connections", Desc: "list all configured connections"},
			{Command: "/connections validate anthropic default", Desc: "check a connection is valid"},
		},
	}
}

func manModel() commandManPage {
	return commandManPage{
		Title:    "MODEL",
		Synopsis: "/model [subcommand]",
		Description: []string{
			"Show model status or manage model configuration.",
			"Without a subcommand, shows the current model config.",
		},
		Subcommands: []manSubcommand{
			{Name: "setup", Desc: "run the interactive model setup wizard"},
			{Name: "validate", Desc: "validate the current model config via Loopgate"},
		},
		Examples: []manExample{
			{Command: "/model", Desc: "show current model provider and settings"},
			{Command: "/model setup", Desc: "reconfigure the model connection"},
			{Command: "/model validate", Desc: "test the current config is valid"},
		},
	}
}

func manQuarantine() commandManPage {
	return commandManPage{
		Title:    "QUARANTINE",
		Synopsis: "/quarantine <subcommand> <quarantine-ref>",
		Description: []string{
			"Inspect, view, or prune content that was quarantined",
			"by Loopgate's safety pipeline.",
		},
		Subcommands: []manSubcommand{
			{Name: "metadata", Args: "<ref>", Desc: "show quarantine metadata"},
			{Name: "view", Args: "<ref>", Desc: "explicitly view quarantined content"},
			{Name: "prune", Args: "<ref>", Desc: "permanently delete quarantined content"},
		},
		Examples: []manExample{
			{Command: "/quarantine metadata qr_abc123"},
			{Command: "/quarantine view qr_abc123"},
			{Command: "/quarantine prune qr_abc123"},
		},
		Notes: []string{
			"Viewing quarantined content is logged. Pruning is irreversible.",
		},
	}
}

func manDebug() commandManPage {
	return commandManPage{
		Title:    "DEBUG",
		Synopsis: "/debug <subcommand> [args...]",
		Description: []string{
			"Diagnostic subcommands for troubleshooting.",
		},
		Subcommands: []manSubcommand{
			{Name: "help", Desc: "show debug command list"},
			{Name: "safepath", Args: "<path>", Desc: "print SafePath decision trail"},
		},
		Examples: []manExample{
			{Command: "/debug safepath /etc/passwd"},
		},
	}
}

func manWrite() commandManPage {
	return commandManPage{
		Title:    "WRITE",
		Synopsis: "/write <file> <text...>",
		Description: []string{
			"Write content to a file through Loopgate.",
			"This operation is approval-gated by policy.",
		},
		Examples: []manExample{
			{Command: "/write hello.txt Hello, world!"},
			{Command: "/write src/main.go package main"},
		},
		Notes: []string{
			"Writes go through Loopgate's capability pipeline and",
			"are subject to filesystem policy (allowed roots, denied paths).",
		},
	}
}

func manLs() commandManPage {
	return commandManPage{
		Title:    "LS",
		Synopsis: "/ls [path]",
		Description: []string{
			"List directory contents through Loopgate.",
			"Defaults to the working root if no path given.",
		},
		Examples: []manExample{
			{Command: "/ls", Desc: "list the working root"},
			{Command: "/ls src/", Desc: "list the src directory"},
		},
	}
}

func manCat() commandManPage {
	return commandManPage{
		Title:    "CAT",
		Synopsis: "/cat <file>",
		Description: []string{
			"Read and display a file through Loopgate.",
		},
		Examples: []manExample{
			{Command: "/cat README.md"},
			{Command: "/cat src/main.go"},
		},
	}
}

func manHelp() commandManPage {
	return commandManPage{
		Title:    "HELP",
		Synopsis: "/help",
		Description: []string{
			"Show a summary of all available commands.",
		},
		Examples: []manExample{
			{Command: "/help", Desc: "list all commands"},
		},
	}
}

func manMan() commandManPage {
	return commandManPage{
		Title:    "MAN",
		Synopsis: "/man <command>",
		Description: []string{
			"Show the detailed man page for a command.",
			"The command argument should include the leading slash.",
		},
		Examples: []manExample{
			{Command: "/man /sandbox", Desc: "show the sandbox man page"},
		},
	}
}

func manExit() commandManPage {
	return commandManPage{
		Title:    "EXIT",
		Synopsis: "/exit",
		Description: []string{
			"End the current session. Alias: /quit.",
		},
		Examples: []manExample{
			{Command: "/exit", Desc: "end the session"},
		},
	}
}

func manReset() commandManPage {
	return commandManPage{
		Title:    "RESET",
		Synopsis: "/reset",
		Description: []string{
			"Start a new session while preserving the audit ledger.",
			"Conversation context is cleared but durable state remains.",
		},
		Examples: []manExample{
			{Command: "/reset", Desc: "start a fresh session"},
		},
	}
}

func manPwd() commandManPage {
	return commandManPage{
		Title:    "PWD",
		Synopsis: "/pwd",
		Description: []string{
			"Print the current working root directory.",
		},
		Examples: []manExample{
			{Command: "/pwd", Desc: "show the working root"},
		},
	}
}

func manSetup() commandManPage {
	return commandManPage{
		Title:    "SETUP",
		Synopsis: "/setup",
		Description: []string{
			"Run the interactive model setup wizard.",
			"Guides you through configuring a model provider and connection.",
		},
		Examples: []manExample{
			{Command: "/setup", Desc: "launch the setup wizard"},
		},
	}
}

func manAgent() commandManPage {
	return commandManPage{
		Title:    "AGENT",
		Synopsis: "/agent",
		Description: []string{
			"Show a summary of the current agent and runtime state.",
		},
		Examples: []manExample{
			{Command: "/agent", Desc: "display agent/runtime info"},
		},
	}
}

func manPersona() commandManPage {
	return commandManPage{
		Title:    "PERSONA",
		Synopsis: "/persona",
		Description: []string{
			"Show a summary of the active persona configuration.",
		},
		Examples: []manExample{
			{Command: "/persona", Desc: "display current persona"},
		},
	}
}

func manSettings() commandManPage {
	return commandManPage{
		Title:    "SETTINGS",
		Synopsis: "/settings",
		Description: []string{
			"Show the effective settings for the current session.",
		},
		Examples: []manExample{
			{Command: "/settings", Desc: "display effective settings"},
		},
	}
}

func manNetwork() commandManPage {
	return commandManPage{
		Title:    "NETWORK",
		Synopsis: "/network",
		Description: []string{
			"Show the current network posture managed by Loopgate.",
		},
		Examples: []manExample{
			{Command: "/network", Desc: "display network posture"},
		},
	}
}

func manConfig() commandManPage {
	return commandManPage{
		Title:    "CONFIG",
		Synopsis: "/config",
		Description: []string{
			"Show the paths to active configuration files.",
		},
		Examples: []manExample{
			{Command: "/config", Desc: "list config file paths"},
		},
	}
}

func manTools() commandManPage {
	return commandManPage{
		Title:    "TOOLS",
		Synopsis: "/tools",
		Description: []string{
			"Show all registered tools available in the current session.",
		},
		Examples: []manExample{
			{Command: "/tools", Desc: "list registered tools"},
		},
	}
}

func manPolicy() commandManPage {
	return commandManPage{
		Title:    "POLICY",
		Synopsis: "/policy",
		Description: []string{
			"Show a summary of loaded policies governing the session.",
		},
		Examples: []manExample{
			{Command: "/policy", Desc: "display policy summary"},
		},
	}
}

// commandManPages returns the man page lookup table.
func commandManPages() map[string]commandManPage {
	return map[string]commandManPage{
		"/sandbox":     manSandbox(),
		"/site":        manSite(),
		"/connections": manConnections(),
		"/model":       manModel(),
		"/quarantine":  manQuarantine(),
		"/debug":       manDebug(),
		"/write":       manWrite(),
		"/ls":          manLs(),
		"/cat":         manCat(),
		"/help":        manHelp(),
		"/man":         manMan(),
		"/exit":        manExit(),
		"/reset":       manReset(),
		"/pwd":         manPwd(),
		"/setup":       manSetup(),
		"/agent":       manAgent(),
		"/persona":     manPersona(),
		"/settings":    manSettings(),
		"/network":     manNetwork(),
		"/config":      manConfig(),
		"/tools":       manTools(),
		"/policy":      manPolicy(),
	}
}

// LookupManPage returns the rendered man page for a command, if available.
func LookupManPage(command string) (string, bool) {
	pages := commandManPages()
	page, found := pages[command]
	if !found {
		return "", false
	}
	return renderManPage(page), true
}
