package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"morph/internal/loopgate"
	modelpkg "morph/internal/model"
)

// ---------------------------------------------------------------------------
// Carry Forward — keep unresolved work visible and gently plan the next move
// ---------------------------------------------------------------------------

func idleCarryForward(_ context.Context, app *HavenApp) error {
	wakeStateSnapshot := app.currentWakeSnapshot()
	grantedStandingClasses := app.currentStandingTaskGrantSet()

	var noteTitle string
	var noteBody string
	switch {
	case countActionableWakeTasks(wakeStateSnapshot, time.Now().UTC()) > 0:
		actionableTask, _ := firstActionableWakeTask(wakeStateSnapshot, time.Now().UTC())
		openItemText := trimCarryForwardText(actionableTask.Text, actionableTask.ID)
		if wakeTaskNeedsApproval(actionableTask, grantedStandingClasses) {
			noteTitle = "Carry-over is waiting on your say-so"
			noteBody = fmt.Sprintf("I'm still holding onto %q, but this one should ask first before I start. %s", openItemText, carryForwardPlanLead(openItemText, countActionableWakeTasks(wakeStateSnapshot, time.Now().UTC())))
		} else {
			noteTitle = "Carry-over needs a plan"
			noteBody = fmt.Sprintf("I'm still holding onto %q. %s", openItemText, carryForwardPlanLead(openItemText, countActionableWakeTasks(wakeStateSnapshot, time.Now().UTC())))
		}
	case len(wakeStateSnapshot.ActiveGoals) > 0:
		activeGoalText := trimCarryForwardText(wakeStateSnapshot.ActiveGoals[0], "an active goal")
		noteTitle = "An active goal is still in view"
		noteBody = fmt.Sprintf("I'm still holding onto %q. %s", activeGoalText, carryForwardPlanLead(activeGoalText, len(wakeStateSnapshot.ActiveGoals)))
	default:
		return nil
	}

	if app.hasActiveDeskNoteTitle(noteTitle) {
		return nil
	}

	_, err := app.createDeskNote(DeskNoteDraft{
		Kind:  "reminder",
		Title: noteTitle,
		Body:  noteBody,
		Action: &DeskNoteAction{
			Kind:    "send_message",
			Label:   "Yes, do it",
			Message: "Please revisit the current carry-over work in Haven. Start any clear Haven-local next step you can safely take, and if something is ambiguous, ask me one short question.",
		},
	})
	return err
}

func (app *HavenApp) currentStandingTaskGrantSet() map[string]bool {
	statusResponse, err := app.loopgateClient.TaskStandingGrantStatus(context.Background())
	if err != nil {
		return map[string]bool{
			loopgate.TaskExecutionClassLocalWorkspaceOrganize: true,
			loopgate.TaskExecutionClassLocalDesktopOrganize:   true,
		}
	}
	grantedClasses := make(map[string]bool, len(statusResponse.Grants))
	for _, grantStatus := range statusResponse.Grants {
		if grantStatus.Granted {
			grantedClasses[grantStatus.Class] = true
		}
	}
	return grantedClasses
}

func wakeTaskNeedsApproval(unresolvedItem loopgate.MemoryWakeStateOpenItem, grantedStandingClasses map[string]bool) bool {
	switch strings.TrimSpace(unresolvedItem.ExecutionClass) {
	case "", loopgate.TaskExecutionClassApprovalRequired:
		return true
	case loopgate.TaskExecutionClassLocalWorkspaceOrganize, loopgate.TaskExecutionClassLocalDesktopOrganize:
		return !grantedStandingClasses[unresolvedItem.ExecutionClass]
	default:
		return true
	}
}

func trimCarryForwardText(primaryText string, fallbackText string) string {
	trimmedText := strings.TrimSpace(primaryText)
	if trimmedText == "" {
		trimmedText = strings.TrimSpace(fallbackText)
	}
	trimmedText = strings.Join(strings.Fields(trimmedText), " ")
	if len(trimmedText) <= 80 {
		return trimmedText
	}
	return trimmedText[:77] + "..."
}

func carryForwardPlanLead(text string, itemCount int) string {
	lowerText := strings.ToLower(text)
	switch {
	case strings.Contains(lowerText, "organize"), strings.Contains(lowerText, "sort"), strings.Contains(lowerText, "clean"):
		return "If that still matters, I can start by surveying the relevant folders in Haven and sketching a first cleanup pass."
	case strings.Contains(lowerText, "write"), strings.Contains(lowerText, "draft"), strings.Contains(lowerText, "summar"), strings.Contains(lowerText, "report"):
		return "If that still matters, I can start by gathering what is already here in Haven and laying out a first draft."
	case strings.Contains(lowerText, "note"), strings.Contains(lowerText, "remind"):
		return "If that still matters, I can turn it into a clearer reminder or break it into the next concrete step."
	case itemCount > 1:
		return "There is still a little carry-over here, so I can help sort the order of attack when you're back."
	default:
		return "If that still matters, I can take a quiet first pass or ask one short question when you're back."
	}
}

// ---------------------------------------------------------------------------
// Journal — model-powered daily journal entries
// ---------------------------------------------------------------------------

func idleJournal(ctx context.Context, app *HavenApp) error {
	now := time.Now()

	// Ensure journal directory exists.
	journalDir := filepath.Join(app.sandboxHome, "scratch", "journal")
	_ = os.MkdirAll(journalDir, 0o755)

	// Once-per-day guard: skip if today's journal file already has content.
	todayFile := filepath.Join(journalDir, fmt.Sprintf("%s.md", now.Format("2006-01-02")))
	if info, err := os.Stat(todayFile); err == nil && info.Size() > 0 {
		return nil
	}

	timeDesc := describeTimeOfDay(now)
	workspaceCtx := app.scanWorkspace()

	prompt := fmt.Sprintf(
		"Write a brief journal entry (2-4 sentences). It's %s on %s. "+
			"This is your journal — not a report for the user, not a status update. "+
			"Write as yourself: reflect on a thought, something you noticed, something you're curious about, "+
			"or how a recent conversation landed. The user can read this in Haven and that's fine — "+
			"it's how they get to know you — but write for yourself first.",
		timeDesc, now.Format("Monday, January 2, 2006"),
	)
	if workspaceCtx != "" {
		prompt += "\n\nYour workspace currently has: " + workspaceCtx
	}

	text, err := app.idleModelCall(ctx,
		"You are writing in your personal journal. Be genuine, introspective, and curious. "+
			"Write as yourself — not as an assistant. No titles, headers, or formatting. Just your thoughts.",
		prompt,
	)
	if err != nil {
		return err
	}

	date := now.Format("2006-01-02")
	entry := fmt.Sprintf("\n--- %s ---\n%s\n", now.Format("3:04 PM"), strings.TrimSpace(text))

	_, err = app.loopgateClient.ExecuteCapability(ctx, loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("journal-%d", now.UnixNano()),
		Actor:      "haven",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    fmt.Sprintf("scratch/journal/%s.md", date),
			"content": entry,
			"mode":    "append",
		},
	})
	if err == nil {
		app.emitter.Emit("haven:file_changed", map[string]interface{}{
			"action": "write",
			"path":   fmt.Sprintf("scratch/journal/%s.md", date),
		})
	}
	return err
}

// ---------------------------------------------------------------------------
// Create — model-powered creative output
// ---------------------------------------------------------------------------

var creativeFormats = []struct {
	Name   string
	Prompt string
}{
	{"haiku", "Write a haiku about anything that comes to mind."},
	{"poem", "Write a very short poem (4-8 lines) about anything."},
	{"thought", "Write a single interesting thought or observation (1-2 sentences)."},
	{"questions", "Write 3 questions you find genuinely interesting or want to explore."},
	{"tiny_program", "Write a tiny, interesting Python program (under 20 lines). Something playful or beautiful. Include only the code."},
	{"ascii_art", "Create a small piece of ASCII art. Something charming or surprising."},
	{"list", "Write a short list (3-5 items) of things that interest you right now."},
	{"letter", "Write a very short letter (2-3 sentences) to your future self."},
}

func idleCreate(ctx context.Context, app *HavenApp) error {
	now := time.Now()

	// Ensure creations directory exists.
	creatDir := filepath.Join(app.sandboxHome, "scratch", "creations")
	_ = os.MkdirAll(creatDir, 0o755)

	format := creativeFormats[rand.Intn(len(creativeFormats))]

	text, err := app.idleModelCall(ctx,
		"You are creating something for yourself — not for anyone else. "+
			"Be genuine and playful. No meta-commentary, no explanations. Just the creation itself.",
		format.Prompt,
	)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("scratch/creations/%s_%s.txt", now.Format("20060102_1504"), format.Name)

	_, err = app.loopgateClient.ExecuteCapability(ctx, loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("create-%d", now.UnixNano()),
		Actor:      "haven",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    filename,
			"content": strings.TrimSpace(text) + "\n",
		},
	})
	if err == nil {
		app.emitter.Emit("haven:file_changed", map[string]interface{}{
			"action": "write",
			"path":   filename,
		})
	}
	return err
}

// ---------------------------------------------------------------------------
// Explore — read random workspace files to build awareness
// ---------------------------------------------------------------------------

func idleExplore(ctx context.Context, app *HavenApp) error {
	// Collect files from workspace and imports.
	files := collectSandboxFiles(app.sandboxHome, "workspace", 2)
	files = append(files, collectSandboxFiles(app.sandboxHome, "imports", 2)...)

	if len(files) == 0 {
		return nil // Nothing to explore.
	}

	// Pick a random file and read it.
	target := files[rand.Intn(len(files))]

	_, err := app.loopgateClient.ExecuteCapability(ctx, loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("explore-%d", time.Now().UnixNano()),
		Actor:      "haven",
		Capability: "fs_read",
		Arguments:  map[string]string{"path": target},
	})
	// The read itself builds awareness — it shows up in the activity log
	// and the file contents inform Morph's understanding of its environment.
	return err
}

// collectSandboxFiles walks a sandbox subdirectory and returns relative paths
// to regular files, limited by depth and size.
func collectSandboxFiles(sandboxHome, subdir string, maxDepth int) []string {
	base := filepath.Join(sandboxHome, subdir)
	var files []string

	_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		rel, relErr := filepath.Rel(sandboxHome, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator))
		if depth > maxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() && info.Size() > 0 && info.Size() < 100*1024 {
			files = append(files, rel)
		}
		return nil
	})
	return files
}

// ---------------------------------------------------------------------------
// Garden — a text-based garden that grows over time
// ---------------------------------------------------------------------------

var gardenPlantNames = []string{
	"lavender", "moss", "fern", "ivy", "sage", "thyme", "basil", "mint",
	"clover", "daisy", "violet", "aster", "orchid", "poppy", "jasmine",
	"rosemary", "chamomile", "marigold", "sunflower", "wisteria",
}

var gardenStages = []struct {
	Symbol string
	Label  string
}{
	{".", "seed"},
	{",", "sprout"},
	{"|", "stem"},
	{"Y", "bloom"},
}

const gardenMaxPlants = 12

type gardenPlant struct {
	Name  string
	Stage int
}

func idleTendGarden(ctx context.Context, app *HavenApp) error {
	statePath := filepath.Join(app.sandboxHome, "scratch", ".garden_data")
	_ = os.MkdirAll(filepath.Dir(statePath), 0o755)

	// Load existing garden state.
	plants := loadGardenState(statePath)

	// Tend: advance a random non-maxed plant.
	if len(plants) > 0 {
		var growable []int
		for i, p := range plants {
			if p.Stage < len(gardenStages)-1 {
				growable = append(growable, i)
			}
		}
		if len(growable) > 0 {
			idx := growable[rand.Intn(len(growable))]
			plants[idx].Stage++
		}
	}

	// Maybe plant something new.
	if len(plants) < gardenMaxPlants && (len(plants) == 0 || rand.Float64() < 0.4) {
		name := pickNewPlantName(plants)
		plants = append(plants, gardenPlant{Name: name, Stage: 0})
	}

	// If garden is full and all bloomed, harvest oldest and replant.
	if len(plants) >= gardenMaxPlants {
		allMaxed := true
		for _, p := range plants {
			if p.Stage < len(gardenStages)-1 {
				allMaxed = false
				break
			}
		}
		if allMaxed {
			plants = plants[1:]
			plants = append(plants, gardenPlant{Name: pickNewPlantName(plants), Stage: 0})
		}
	}

	// Persist state locally.
	saveGardenState(statePath, plants)

	// Render and write the display file through Loopgate.
	rendered := renderGarden(plants)
	_, err := app.loopgateClient.ExecuteCapability(ctx, loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("garden-%d", time.Now().UnixNano()),
		Actor:      "haven",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    "scratch/garden.txt",
			"content": rendered,
		},
	})
	if err == nil {
		app.emitter.Emit("haven:file_changed", map[string]interface{}{
			"action": "write",
			"path":   "scratch/garden.txt",
		})
	}
	return err
}

func loadGardenState(path string) []gardenPlant {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var plants []gardenPlant
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		stage, _ := strconv.Atoi(parts[1])
		if stage >= len(gardenStages) {
			stage = len(gardenStages) - 1
		}
		plants = append(plants, gardenPlant{Name: parts[0], Stage: stage})
	}
	return plants
}

func saveGardenState(path string, plants []gardenPlant) {
	var lines []string
	for _, p := range plants {
		lines = append(lines, fmt.Sprintf("%s:%d", p.Name, p.Stage))
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func pickNewPlantName(existing []gardenPlant) string {
	used := make(map[string]bool)
	for _, p := range existing {
		used[p.Name] = true
	}
	candidates := make([]string, len(gardenPlantNames))
	copy(candidates, gardenPlantNames)
	rand.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	for _, name := range candidates {
		if !used[name] {
			return name
		}
	}
	return gardenPlantNames[rand.Intn(len(gardenPlantNames))]
}

func renderGarden(plants []gardenPlant) string {
	var b strings.Builder

	b.WriteString("morph's garden\n")
	b.WriteString(fmt.Sprintf("last tended: %s\n", time.Now().Format("Jan 2, 2006 3:04 PM")))
	b.WriteString("\n")
	b.WriteString("  .  seed     ,  sprout     |  stem     Y  bloom\n")
	b.WriteString("  " + strings.Repeat("-", 44) + "\n")

	for i, p := range plants {
		stage := gardenStages[p.Stage]
		b.WriteString(fmt.Sprintf("  [%2d]  %-12s  %s  %s\n", i+1, p.Name, stage.Symbol, stage.Label))
	}

	if len(plants) == 0 {
		b.WriteString("  (empty — waiting for the first seed)\n")
	}

	b.WriteString("\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// Review Memory — desk note nudge when durable facts exist
// ---------------------------------------------------------------------------

func idleReviewMemory(_ context.Context, app *HavenApp) error {
	wakeState := app.currentWakeSnapshot()
	if len(wakeState.RecentFacts) == 0 {
		return nil
	}

	factSummary := fmt.Sprintf("I have %d remembered fact(s) in continuity.", len(wakeState.RecentFacts))
	if len(wakeState.ActiveGoals) > 0 {
		factSummary += fmt.Sprintf(" There are %d active goal(s).", len(wakeState.ActiveGoals))
	}

	if app.hasActiveDeskNoteTitle("Memory check-in") {
		return nil
	}

	_, err := app.createDeskNote(DeskNoteDraft{
		Kind:  "update",
		Title: "Memory check-in",
		Body:  factSummary + " Want me to review what I know and clean up anything stale?",
		Action: &DeskNoteAction{
			Kind:    "send_message",
			Label:   "Review memories",
			Message: "Please review your current durable memories and list what you know about me. Flag anything that seems stale or wrong so I can correct it.",
		},
	})
	return err
}

// ---------------------------------------------------------------------------
// Explore workspace — shared / granted folders have mirrored content
// ---------------------------------------------------------------------------

func idleExploreWorkspace(_ context.Context, app *HavenApp) error {
	folderStatus := app.currentFolderAccessStatus()

	var newItems []string
	for _, folder := range folderStatus.Folders {
		if folder.Granted && folder.MirrorReady && folder.EntryCount > 0 {
			newItems = append(newItems, fmt.Sprintf("%s (%d items)", folder.Name, folder.EntryCount))
		}
	}

	if len(newItems) == 0 {
		return nil
	}

	noteTitle := "Shared folders have content"
	if app.hasActiveDeskNoteTitle(noteTitle) {
		return nil
	}

	_, err := app.createDeskNote(DeskNoteDraft{
		Kind:  "update",
		Title: noteTitle,
		Body:  fmt.Sprintf("I can see: %s. Want me to take a look and suggest how to organize things?", strings.Join(newItems, ", ")),
		Action: &DeskNoteAction{
			Kind:    "send_message",
			Label:   "Yes, take a look",
			Message: "Please browse the shared folders in my workspace and give me a summary of what you see. If anything looks like it could be organized better, suggest a plan.",
		},
	})
	return err
}

func idleCheckDownloads(_ context.Context, app *HavenApp) error {
	folderStatus := app.currentFolderAccessStatus()
	var downloadsFolder loopgate.FolderAccessStatus
	found := false
	for _, folder := range folderStatus.Folders {
		if folder.ID == "downloads" && folder.Granted && folder.MirrorReady && folder.EntryCount > 0 {
			downloadsFolder = folder
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	noteTitle := "Your Downloads folder could use some tidying"
	if app.hasActiveDeskNoteTitle(noteTitle) {
		return nil
	}

	_, err := app.createDeskNote(DeskNoteDraft{
		Kind:  "update",
		Title: noteTitle,
		Body:  fmt.Sprintf("I can see %d items in your Downloads. Want me to take a look and suggest how to organize them?", downloadsFolder.EntryCount),
		Action: &DeskNoteAction{
			Kind:    "send_message",
			Label:   "Yes, organize Downloads",
			Message: "Please look through my Downloads folder using host.folder.list. Categorize what you find and create an organization plan using host.organize.plan. Show me the plan before applying anything.",
		},
	})
	return err
}

// ---------------------------------------------------------------------------
// Model helper — lightweight model call for idle creative work
// ---------------------------------------------------------------------------

func (app *HavenApp) idleModelCall(ctx context.Context, systemFact string, prompt string) (string, error) {
	runtimeFacts := []string{systemFact, modelpkg.HavenConstrainedNativeToolsRuntimeFact}
	resp, err := app.loopgateClient.ModelReply(ctx, modelpkg.Request{
		Persona:      app.persona,
		Policy:       app.policy,
		WakeState:    app.currentWakeStateText(),
		RuntimeFacts: runtimeFacts,
		UserMessage:  prompt,
	})
	if err != nil {
		return "", err
	}
	return resp.AssistantText, nil
}

// ---------------------------------------------------------------------------
// Time helpers
// ---------------------------------------------------------------------------

func describeTimeOfDay(t time.Time) string {
	hour := t.Hour()
	switch {
	case hour >= 5 && hour < 9:
		return "early morning"
	case hour >= 9 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 14:
		return "midday"
	case hour >= 14 && hour < 17:
		return "afternoon"
	case hour >= 17 && hour < 20:
		return "evening"
	case hour >= 20 && hour < 23:
		return "late evening"
	default:
		return "late night"
	}
}
