package shell

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"morph/internal/config"
	"morph/internal/safety"
)

// NewCompleter returns a readline AutoCompleter with command + policy-aware path completion.
func NewCompleter(repoRoot string, policy config.Policy) *MorphCompleter {
	return &MorphCompleter{
		RepoRoot: repoRoot,
		Policy:   policy,
		Commands: append(commandNames(), "/quit"),
	}
}

type MorphCompleter struct {
	RepoRoot string
	Policy   config.Policy
	Commands []string
}

func (c *MorphCompleter) Do(line []rune, pos int) ([][]rune, int) {
	input := string(line[:pos])
	trim := strings.TrimLeft(input, " ")
	if !strings.HasPrefix(trim, "/") {
		return nil, 0
	}

	lastSpace := strings.LastIndexAny(input, " \t")
	var token string
	if lastSpace == -1 {
		token = input
	} else {
		token = input[lastSpace+1:]
	}

	fields := strings.Fields(input)
	if len(fields) == 0 {
		return nil, 0
	}
	cmd := strings.ToLower(fields[0])

	switch cmd {
	case "/debug":
		// Subcommand completion for: /debug help|safepath
		debugSubs := []string{"help", "safepath"}

		// If we're completing the second token (or user just typed `/debug `), suggest subcommands.
		// Note: strings.Fields drops trailing spaces, so token may be "" when the cursor is after a space.
		if len(fields) == 1 {
			full := completePrefix(token, debugSubs)
			suffixes := make([]string, 0, len(full))
			for _, opt := range full {
				if strings.HasPrefix(opt, token) {
					suffixes = append(suffixes, opt[len(token):])
				}
			}
			return toRunes(suffixes), 0
		}

		sub := strings.ToLower(fields[1])

		// If subcommand is `safepath`, complete file paths as the next argument.
		// Works both when user is typing the path (len(fields)>=3) and when they've typed a trailing space
		// after `safepath` (len(fields)==2 but token is empty).
		if sub == "safepath" {
			return c.completePathSuffix(token, false), 0
		}

		// Otherwise, if user is still typing the subcommand (e.g. `/debug sa`), complete it.
		// We only do this when there are exactly 2 fields, so we don't fight with path completion.
		if len(fields) == 2 {
			full := completePrefix(token, debugSubs)
			suffixes := make([]string, 0, len(full))
			for _, opt := range full {
				if strings.HasPrefix(opt, token) {
					suffixes = append(suffixes, opt[len(token):])
				}
			}
			return toRunes(suffixes), 0
		}

		return nil, 0
	case "/ls":
		return c.completePathSuffix(token, true), 0
	case "/cat":
		return c.completePathSuffix(token, false), 0
	case "/write":
		return c.completePathSuffix(token, false), 0
	default:
		// Command completion
		if lastSpace == -1 {
			return c.CommandSuffixes(token), 0
		}
		return nil, 0
	}
}

func (c *MorphCompleter) CommandSuffixes(token string) [][]rune {
	full := completePrefix(token, c.Commands)
	suffixes := make([][]rune, 0, len(full))
	for _, option := range full {
		if strings.HasPrefix(option, token) {
			suffixes = append(suffixes, []rune(option[len(token):]))
		}
	}
	return suffixes
}

func completePrefix(prefix string, options []string) []string {
	out := []string{}
	for _, o := range options {
		if strings.HasPrefix(o, prefix) {
			out = append(out, o)
		}
	}
	sort.Strings(out)
	return out
}

func toRunes(opts []string) [][]rune {
	out := make([][]rune, 0, len(opts))
	for _, o := range opts {
		out = append(out, []rune(o))
	}
	return out
}

func (c *MorphCompleter) completePath(userPrefix string, dirsOnly bool) []string {
	prefix := strings.TrimSpace(userPrefix)
	if prefix == "" {
		prefix = "."
	}

	scanDirRel, partial := splitDirBase(prefix)

	scanAbs, err := safety.SafePath(c.RepoRoot, c.Policy.Tools.Filesystem.AllowedRoots, c.Policy.Tools.Filesystem.DeniedPaths, scanDirRel)
	if err != nil {
		return nil
	}

	entries, err := os.ReadDir(scanAbs)
	if err != nil {
		return nil
	}

	sugs := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if partial != "" && !strings.HasPrefix(name, partial) {
			continue
		}

		isDir := e.IsDir()
		if dirsOnly && !isDir {
			continue
		}

		rel := joinRel(scanDirRel, name)
		if _, err := safety.SafePath(c.RepoRoot, c.Policy.Tools.Filesystem.AllowedRoots, c.Policy.Tools.Filesystem.DeniedPaths, rel); err != nil {
			continue
		}

		if isDir {
			rel += "/"
		}
		sugs = append(sugs, rel)
	}

	sort.Strings(sugs)
	return sugs
}

func splitDirBase(path string) (dirRel string, base string) {
	path = strings.TrimSpace(path)

	if strings.HasSuffix(path, "/") {
		dir := strings.TrimSuffix(path, "/")
		if dir == "" {
			return ".", ""
		}
		return dir, ""
	}

	if strings.Contains(path, "/") {
		dir := filepath.Dir(path)
		base := filepath.Base(path)
		if dir == "" || dir == "." {
			return ".", base
		}
		return filepath.ToSlash(dir), base
	}
	return ".", path
}

func joinRel(dirRel, name string) string {
	if dirRel == "" || dirRel == "." {
		return name
	}
	return filepath.ToSlash(filepath.Join(dirRel, name))
}

func (c *MorphCompleter) completePathSuffix(token string, dirsOnly bool) [][]rune {
	full := c.completePath(token, dirsOnly)
	suffixes := make([][]rune, 0, len(full))
	for _, candidate := range full {
		if strings.HasPrefix(candidate, token) {
			suffixes = append(suffixes, []rune(candidate[len(token):]))
		}
	}
	return suffixes
}
