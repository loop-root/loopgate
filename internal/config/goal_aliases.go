package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const goalAliasesVersion = "1"

type GoalAliases struct {
	Version string              `yaml:"version" json:"version"`
	Aliases map[string][]string `yaml:"aliases" json:"aliases"`
}

func LoadGoalAliases(repoRoot string) (GoalAliases, error) {
	path := filepath.Join(repoRoot, "config", "goal_aliases.yaml")
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultGoalAliases(), nil
		}
		return GoalAliases{}, err
	}
	var goalAliases GoalAliases
	decoder := yaml.NewDecoder(bytes.NewReader(rawBytes))
	decoder.KnownFields(true)
	if err := decoder.Decode(&goalAliases); err != nil {
		return GoalAliases{}, err
	}
	if strings.TrimSpace(goalAliases.Version) == "" {
		goalAliases.Version = goalAliasesVersion
	}
	if err := validateGoalAliases(goalAliases); err != nil {
		return GoalAliases{}, err
	}
	return goalAliases, nil
}

// DefaultGoalAliases returns the default goal alias mappings.
func DefaultGoalAliases() GoalAliases {
	return GoalAliases{
		Version: goalAliasesVersion,
		Aliases: map[string][]string{
			"technical_review": {
				"rfc_review",
				"design_doc_review",
				"implementation_concern_synthesis",
			},
			"architecture_planning": {
				"architecture_session",
				"system_design",
				"design_planning",
			},
			"security_hardening": {
				"security_pass",
				"hardening_pass",
				"threat_review",
			},
			"documentation_update": {
				"readme_update",
				"docs_revision",
				"rfc_edit",
			},
			"debugging_investigation": {
				"incident_debug",
				"failure_analysis",
				"bug_investigation",
			},
			"workflow_followup": {
				"status_followup",
				"handoff_followup",
				"todo_followup",
			},
			"scheduling_commitment": {
				"deadline_tracking",
				"calendar_commitment",
				"reminder_tracking",
			},
			"research_synthesis": {
				"options_comparison",
				"recommendation_brief",
				"research_summary",
			},
			"preference_or_config_update": {
				"preference_change",
				"config_tuning",
				"setup_update",
			},
		},
	}
}

func validateGoalAliases(goalAliases GoalAliases) error {
	if strings.TrimSpace(goalAliases.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if len(goalAliases.Aliases) == 0 {
		return fmt.Errorf("aliases is required")
	}
	for goalType, rawAliases := range goalAliases.Aliases {
		if strings.TrimSpace(goalType) == "" {
			return fmt.Errorf("goal type is required")
		}
		if len(rawAliases) == 0 {
			return fmt.Errorf("aliases for %q are required", goalType)
		}
		seenAliases := map[string]struct{}{}
		for _, rawAlias := range rawAliases {
			normalizedAlias := normalizeGoalAlias(rawAlias)
			if normalizedAlias == "" {
				return fmt.Errorf("blank alias for %q", goalType)
			}
			if _, found := seenAliases[normalizedAlias]; found {
				return fmt.Errorf("duplicate alias %q for %q", normalizedAlias, goalType)
			}
			seenAliases[normalizedAlias] = struct{}{}
		}
	}
	return nil
}

func normalizeGoalAlias(rawAlias string) string {
	normalizedAlias := strings.ToLower(strings.TrimSpace(rawAlias))
	normalizedAlias = strings.ReplaceAll(normalizedAlias, "-", "_")
	normalizedAlias = strings.ReplaceAll(normalizedAlias, " ", "_")
	return normalizedAlias
}

func NormalizeGoalAliasPublic(rawAlias string) string {
	return normalizeGoalAlias(rawAlias)
}

func GoalTypeKeys(goalAliases GoalAliases) []string {
	goalTypeKeys := make([]string, 0, len(goalAliases.Aliases))
	for goalType := range goalAliases.Aliases {
		goalTypeKeys = append(goalTypeKeys, goalType)
	}
	sort.Strings(goalTypeKeys)
	return goalTypeKeys
}
