package shell

type slashCommandInfo struct {
	Name        string
	Args        string
	ShortArgs   string // compact args for help panel; falls back to Args if empty
	Description string
	ShortDesc   string // compact description for help panel; falls back to Description if empty
}

type PromptCommandDefinition struct {
	Name        string
	Args        string
	Description string
}

func commandCatalog() []slashCommandInfo {
	return []slashCommandInfo{
		{Name: "/help", Args: "", Description: "show command summary"},
		{Name: "/man", Args: "<command>", Description: "show detailed help for a command"},
		{Name: "/exit", Args: "", Description: "end session (alias: /quit)"},
		{Name: "/reset", Args: "", Description: "new session, preserve ledger"},
		{Name: "/pwd", Args: "", Description: "print working root"},
		{Name: "/ls", Args: "[path]", Description: "list directory"},
		{Name: "/cat", Args: "<file>", Description: "read file"},
		{Name: "/write", Args: "<file> <text>", Description: "write file (approval-gated)"},
		{Name: "/setup", Args: "", Description: "run model setup wizard"},
		{Name: "/agent", Args: "", Description: "show agent/runtime summary"},
		{Name: "/model", Args: "[setup|validate]", Description: "show model status or run setup"},
		{Name: "/persona", Args: "", Description: "show persona summary"},
		{Name: "/settings", Args: "", Description: "show effective settings"},
		{Name: "/network", Args: "", Description: "show network posture"},
		{Name: "/connections", Args: "[validate|pkce-start|pkce-complete]", ShortArgs: "<subcommand>", Description: "show or manage Loopgate connections", ShortDesc: "manage Loopgate connections"},
		{Name: "/site", Args: "[inspect|trust-draft] <url>", ShortArgs: "<subcommand> <url>", Description: "inspect a site or create a public_read trust draft", ShortDesc: "inspect or trust a site"},
		{Name: "/sandbox", Args: "[import|stage|metadata|export] ...", ShortArgs: "<subcommand> ...", Description: "import into, stage inside, review, or export from the sandbox", ShortDesc: "manage the content sandbox"},
		{Name: "/morphling", Args: "[spawn|status|review|terminate] ...", ShortArgs: "<subcommand> ...", Description: "manage the local pool of sandbox-scoped morphlings", ShortDesc: "manage morphling workers"},
		{Name: "/quarantine", Args: "[metadata|view|prune] <quarantine-ref>", ShortArgs: "<subcommand> <ref>", Description: "inspect, explicitly view, or prune quarantined content", ShortDesc: "inspect quarantined content"},
		{Name: "/config", Args: "", Description: "show config file paths"},
		{Name: "/tools", Args: "", Description: "show registered tools"},
		{Name: "/memory", Args: "[discover <terms...>|recall <key-id>|remember <fact-key> <value>]", ShortArgs: "<subcommand> ...", Description: "show memory policy, discover keys, recall remembered continuity, or explicitly remember a profile fact", ShortDesc: "memory policy and recall"},
		{Name: "/goal", Args: "add <text> | close [text-or-id] | list", ShortArgs: "<subcommand> ...", Description: "record or inspect explicit active-goal transitions", ShortDesc: "track active goals"},
		{Name: "/todo", Args: "[add|resolve] <text-or-id>", ShortArgs: "<subcommand> ...", Description: "record explicit unresolved-item transitions", ShortDesc: "track unresolved items"},
		{Name: "/policy", Args: "", Description: "show loaded policy summary"},
		{Name: "/debug", Args: "help", Description: "diagnostic subcommands"},
	}
}

func PromptCommandCatalog() []PromptCommandDefinition {
	catalog := commandCatalog()
	commandDefinitions := make([]PromptCommandDefinition, 0, len(catalog))
	for _, commandInfo := range catalog {
		commandDefinitions = append(commandDefinitions, PromptCommandDefinition{
			Name:        commandInfo.Name,
			Args:        commandInfo.Args,
			Description: commandInfo.Description,
		})
	}
	return commandDefinitions
}

func commandNames() []string {
	catalog := commandCatalog()
	names := make([]string, 0, len(catalog))
	for _, command := range catalog {
		names = append(names, command.Name)
	}
	return names
}

