package loopgate

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"morph/internal/sandbox"
)

const (
	havenPaintSandboxDir  = "outputs/paintings"
	maxHavenPaintGallery  = 24
	maxHavenPaintSVGBytes = 512 * 1024
)

// HavenPaintEntry is a single painting in the gallery.
type HavenPaintEntry struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	UpdatedAtUTC string `json:"updated_at_utc"`
	SVG          string `json:"svg"`
}

// HavenPaintGalleryResponse is the payload for GET /v1/ui/paint/gallery.
type HavenPaintGalleryResponse struct {
	Entries []HavenPaintEntry `json:"entries"`
}

func (server *Server) handleHavenPaintGallery(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tokenClaims, ok := server.authenticate(writer, request)
	if !ok {
		return
	}
	if !server.requireControlCapability(writer, tokenClaims, controlCapabilityUIRead) {
		return
	}
	if !server.requireCapabilityScope(writer, tokenClaims, "fs_list") {
		return
	}
	if !server.requireCapabilityScope(writer, tokenClaims, "fs_read") {
		return
	}
	if _, denialResponse, verified := server.verifySignedRequestWithoutBody(request, tokenClaims.ControlSessionID); !verified {
		server.writeJSON(writer, signedRequestHTTPStatus(denialResponse.DenialCode), denialResponse)
		return
	}

	listResponse, err := server.listSandboxDirectory(tokenClaims, SandboxListRequest{SandboxPath: havenPaintSandboxDir})
	if err != nil {
		if errors.Is(err, sandbox.ErrSandboxSourceUnavailable) {
			server.writeJSON(writer, http.StatusOK, HavenPaintGalleryResponse{Entries: nil})
			return
		}
		server.writeJSON(writer, sandboxHTTPStatus(err), CapabilityResponse{
			Status:       ResponseStatusError,
			DenialReason: redactSandboxError(err),
			DenialCode:   sandboxDenialCode(err),
			Redacted:     true,
		})
		return
	}

	type paintBuild struct {
		modTime time.Time
		entry   HavenPaintEntry
	}
	build := make([]paintBuild, 0, len(listResponse.Entries))
	for _, entry := range listResponse.Entries {
		if entry.EntryType != "file" || !strings.HasSuffix(strings.ToLower(entry.Name), ".svg") {
			continue
		}
		sandboxPath := filepath.ToSlash(filepath.Join(havenPaintSandboxDir, entry.Name))
		svg, readErr := server.havenReadFileViaCapability(request.Context(), tokenClaims, sandboxPath)
		if readErr != nil || len(svg) > maxHavenPaintSVGBytes {
			continue
		}
		modTime := parseSandboxJournalModTime(entry.ModTimeUTC)
		build = append(build, paintBuild{
			modTime: modTime,
			entry: HavenPaintEntry{
				Path:         fmt.Sprintf("artifacts/paintings/%s", entry.Name),
				Title:        paintTitleFromFilename(entry.Name),
				UpdatedAtUTC: journalModTimeLocalRFC3339Nano(modTime),
				SVG:          svg,
			},
		})
	}

	sort.Slice(build, func(i, j int) bool {
		zi, zj := build[i].modTime.IsZero(), build[j].modTime.IsZero()
		if zi && zj {
			return false
		}
		if zi {
			return false
		}
		if zj {
			return true
		}
		return build[i].modTime.After(build[j].modTime)
	})

	if len(build) > maxHavenPaintGallery {
		build = build[:maxHavenPaintGallery]
	}

	entries := make([]HavenPaintEntry, 0, len(build))
	for _, row := range build {
		entries = append(entries, row.entry)
	}
	server.writeJSON(writer, http.StatusOK, HavenPaintGalleryResponse{Entries: entries})
}

func paintTitleFromFilename(filename string) string {
	// Format: 20060102-150405-NNNN-slug.svg
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	// Drop leading timestamp portion (up to 3rd hyphen group)
	parts := strings.SplitN(name, "-", 4)
	if len(parts) == 4 {
		slug := parts[3]
		words := strings.Split(slug, "-")
		titled := make([]string, 0, len(words))
		for _, w := range words {
			if w == "" {
				continue
			}
			runes := []rune(w)
			titled = append(titled, strings.ToUpper(string(runes[:1]))+string(runes[1:]))
		}
		if len(titled) > 0 {
			return strings.Join(titled, " ")
		}
	}
	return name
}
