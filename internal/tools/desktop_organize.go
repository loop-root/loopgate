package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DesktopOrganize lets Morph rearrange the desktop icons inside Haven.
type DesktopOrganize struct {
	StateDir string
}

func (t *DesktopOrganize) Name() string      { return "desktop.organize" }
func (t *DesktopOrganize) Category() string  { return "filesystem" }
func (t *DesktopOrganize) Operation() string { return OpWrite }

func (t *DesktopOrganize) Schema() Schema {
	return Schema{
		Description: "Rearrange the desktop icons in Haven. Use a preset layout style or supply custom positions. Icon IDs: morph, loopgate, workspace, activity, todo, notes, journal, paint.",
		Args: []ArgDef{
			{
				Name:        "style",
				Description: "Layout preset: grid-right (default two-column on right), grid-left (two-column on left), row-top (single row across top), or custom (use positions arg)",
				Required:    true,
				Type:        "string",
				MaxLen:      20,
			},
			{
				Name:        "positions",
				Description: "JSON object mapping icon IDs to {x, y} positions. Only used when style is custom. Negative x values align from the right edge. Example: {\"morph\":{\"x\":-110,\"y\":20}}",
				Required:    false,
				Type:        "string",
				MaxLen:      2000,
			},
		},
	}
}

var validIconIDs = map[string]bool{
	"morph": true, "loopgate": true, "workspace": true, "activity": true,
	"todo": true, "notes": true, "journal": true, "paint": true,
}

var iconOrder = []string{"morph", "loopgate", "workspace", "activity", "todo", "notes", "journal", "paint"}

type iconPos struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type iconPositionsFile struct {
	Positions map[string]iconPos `json:"positions"`
}

func (t *DesktopOrganize) Execute(_ context.Context, args map[string]string) (string, error) {
	if strings.TrimSpace(t.StateDir) == "" {
		return "", fmt.Errorf("desktop state directory is not configured")
	}

	style := strings.TrimSpace(args["style"])
	if style == "" {
		style = "grid-right"
	}

	var positions map[string]iconPos

	switch style {
	case "grid-right":
		positions = layoutGridRight()
	case "grid-left":
		positions = layoutGridLeft()
	case "row-top":
		positions = layoutRowTop()
	case "custom":
		rawPositions := strings.TrimSpace(args["positions"])
		if rawPositions == "" {
			return "", fmt.Errorf("positions argument is required when style is custom")
		}
		parsed, err := parseCustomPositions(rawPositions)
		if err != nil {
			return "", err
		}
		positions = parsed
	default:
		return "", fmt.Errorf("unknown layout style %q (use grid-right, grid-left, row-top, or custom)", style)
	}

	if err := t.savePositions(positions); err != nil {
		return "", err
	}

	return fmt.Sprintf("Desktop icons organized (%s layout, %d icons placed)", style, len(positions)), nil
}

func (t *DesktopOrganize) statePath() string {
	return filepath.Join(t.StateDir, "haven_icon_positions.json")
}

func (t *DesktopOrganize) savePositions(positions map[string]iconPos) error {
	state := iconPositionsFile{Positions: positions}
	jsonBytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal icon positions: %w", err)
	}
	if len(jsonBytes) == 0 || jsonBytes[len(jsonBytes)-1] != '\n' {
		jsonBytes = append(jsonBytes, '\n')
	}

	if err := os.MkdirAll(filepath.Dir(t.statePath()), 0o700); err != nil {
		return fmt.Errorf("create icon state dir: %w", err)
	}

	tempPath := t.statePath() + ".tmp"
	if err := os.WriteFile(tempPath, jsonBytes, 0o600); err != nil {
		return fmt.Errorf("write temp icon positions: %w", err)
	}
	if err := os.Rename(tempPath, t.statePath()); err != nil {
		return fmt.Errorf("rename icon positions: %w", err)
	}
	return nil
}

func parseCustomPositions(raw string) (map[string]iconPos, error) {
	var parsed map[string]iconPos
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("invalid positions JSON: %w", err)
	}
	for id := range parsed {
		if !validIconIDs[id] {
			return nil, fmt.Errorf("unknown icon ID %q", id)
		}
	}
	return parsed, nil
}

func layoutGridRight() map[string]iconPos {
	positions := make(map[string]iconPos, len(iconOrder))
	for i, id := range iconOrder {
		col := i % 2 // 0 = outer right, 1 = inner
		row := i / 2
		positions[id] = iconPos{
			X: -(110 + col*100),
			Y: 20 + row*84,
		}
	}
	return positions
}

func layoutGridLeft() map[string]iconPos {
	positions := make(map[string]iconPos, len(iconOrder))
	for i, id := range iconOrder {
		col := i % 2
		row := i / 2
		positions[id] = iconPos{
			X: 20 + col*100,
			Y: 20 + row*84,
		}
	}
	return positions
}

func layoutRowTop() map[string]iconPos {
	positions := make(map[string]iconPos, len(iconOrder))
	for i, id := range iconOrder {
		positions[id] = iconPos{
			X: 20 + i*100,
			Y: 20,
		}
	}
	return positions
}
