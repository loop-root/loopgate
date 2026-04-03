package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"morph/internal/loopgate"
)

const (
	paintGallerySandboxDirectory = "outputs/paintings"
	defaultPaintCanvasWidth      = 960
	defaultPaintCanvasHeight     = 540
	defaultPaintBackground       = "#F6F1E7"
	defaultPaintStrokeColor      = "#3F2D1F"
	maxPaintTitleLength          = 80
	maxPaintGalleryEntries       = 24
	maxPaintStrokes              = 96
	maxPaintPointsPerStroke      = 512
	maxPaintTotalPoints          = 8192
	minPaintCanvasWidth          = 320
	minPaintCanvasHeight         = 240
	maxPaintCanvasDimension      = 2048
)

var paintHexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// PaintPoint is a single point on the Haven Paint canvas.
type PaintPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// PaintStroke is a continuous line drawn in Haven Paint.
type PaintStroke struct {
	Color  string       `json:"color"`
	Width  float64      `json:"width"`
	Points []PaintPoint `json:"points"`
}

// PaintSaveRequest is the structured payload for saving artwork.
type PaintSaveRequest struct {
	Title      string        `json:"title"`
	Width      int           `json:"width"`
	Height     int           `json:"height"`
	Background string        `json:"background"`
	Strokes    []PaintStroke `json:"strokes"`
}

// PaintSaveResponse describes the result of saving artwork.
type PaintSaveResponse struct {
	Saved bool   `json:"saved"`
	Path  string `json:"path,omitempty"`
	Title string `json:"title,omitempty"`
	Error string `json:"error,omitempty"`
}

// PaintArtworkSummary is a gallery card for a saved painting.
type PaintArtworkSummary struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	UpdatedAtUTC string `json:"updated_at_utc"`
	PreviewSVG   string `json:"preview_svg"`
}

// ListPaintings returns recent saved artwork from Haven Paint.
func (app *HavenApp) ListPaintings() ([]PaintArtworkSummary, error) {
	paintRuntimeDirectory := filepath.Join(app.sandboxHome, "outputs", "paintings")
	if app.sandboxHome != "" {
		if _, err := os.Stat(paintRuntimeDirectory); err != nil {
			if os.IsNotExist(err) {
				return []PaintArtworkSummary{}, nil
			}
			return nil, fmt.Errorf("stat paint gallery: %w", err)
		}
	}

	listResponse, err := app.loopgateClient.SandboxList(context.Background(), loopgate.SandboxListRequest{
		SandboxPath: paintGallerySandboxDirectory,
	})
	if err != nil {
		if isMissingPaintGalleryError(err) {
			return []PaintArtworkSummary{}, nil
		}
		return nil, fmt.Errorf("list paint gallery: %w", err)
	}

	paintEntries := make([]loopgate.SandboxListEntry, 0, len(listResponse.Entries))
	for _, entry := range listResponse.Entries {
		if entry.EntryType != "file" || !strings.HasSuffix(strings.ToLower(entry.Name), ".svg") {
			continue
		}
		paintEntries = append(paintEntries, entry)
	}

	sort.Slice(paintEntries, func(leftIndex, rightIndex int) bool {
		return paintEntries[leftIndex].ModTimeUTC > paintEntries[rightIndex].ModTimeUTC
	})

	if len(paintEntries) > maxPaintGalleryEntries {
		paintEntries = paintEntries[:maxPaintGalleryEntries]
	}

	gallery := make([]PaintArtworkSummary, 0, len(paintEntries))
	for _, entry := range paintEntries {
		sandboxPath := filepath.ToSlash(filepath.Join(paintGallerySandboxDirectory, entry.Name))
		svgContent, readErr := app.readPaintingFile(context.Background(), sandboxPath)
		if readErr != nil {
			continue
		}
		gallery = append(gallery, PaintArtworkSummary{
			Path:         mapSandboxPathToHaven(sandboxPath),
			Title:        paintTitleFromSVG(svgContent, paintTitleFromFilename(entry.Name)),
			UpdatedAtUTC: entry.ModTimeUTC,
			PreviewSVG:   svgContent,
		})
	}

	return gallery, nil
}

// PaintSaveArtwork validates structured strokes and saves a synthesized SVG into Haven.
func (app *HavenApp) PaintSaveArtwork(rawRequest PaintSaveRequest) PaintSaveResponse {
	validatedRequest, err := validatePaintSaveRequest(rawRequest)
	if err != nil {
		return PaintSaveResponse{Error: err.Error()}
	}
	if app.sandboxHome == "" {
		return PaintSaveResponse{Error: "sandbox home is not configured"}
	}

	if err := os.MkdirAll(filepath.Join(app.sandboxHome, "outputs", "paintings"), 0o755); err != nil {
		return PaintSaveResponse{Error: fmt.Sprintf("create paint gallery: %v", err)}
	}

	svgContent, err := buildPaintSVG(validatedRequest)
	if err != nil {
		return PaintSaveResponse{Error: fmt.Sprintf("build painting: %v", err)}
	}

	savedAt := time.Now().UTC()
	fileName := buildPaintFilename(validatedRequest.Title, savedAt)
	sandboxPath := filepath.ToSlash(filepath.Join(paintGallerySandboxDirectory, fileName))

	response, err := app.loopgateClient.ExecuteCapability(context.Background(), loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("paint-save-%d", savedAt.UnixNano()),
		Actor:      "haven",
		Capability: "fs_write",
		Arguments: map[string]string{
			"path":    sandboxPath,
			"content": svgContent,
		},
	})
	if err != nil {
		return PaintSaveResponse{Error: fmt.Sprintf("save painting: %v", err)}
	}
	if response.Status != loopgate.ResponseStatusSuccess {
		denialReason := response.DenialReason
		if denialReason == "" {
			denialReason = "painting could not be saved"
		}
		return PaintSaveResponse{Error: denialReason}
	}

	if app.emitter != nil {
		app.emitter.Emit("haven:file_changed", map[string]interface{}{
			"action": "paint_save",
			"path":   sandboxPath,
		})
	}

	return PaintSaveResponse{
		Saved: true,
		Path:  mapSandboxPathToHaven(sandboxPath),
		Title: validatedRequest.Title,
	}
}

func (app *HavenApp) readPaintingFile(ctx context.Context, sandboxPath string) (string, error) {
	response, err := app.loopgateClient.ExecuteCapability(ctx, loopgate.CapabilityRequest{
		RequestID:  fmt.Sprintf("paint-read-%d", time.Now().UnixNano()),
		Actor:      "haven",
		Capability: "fs_read",
		Arguments:  map[string]string{"path": sandboxPath},
	})
	if err != nil {
		return "", fmt.Errorf("read painting: %w", err)
	}
	if response.Status != loopgate.ResponseStatusSuccess {
		denialReason := response.DenialReason
		if denialReason == "" {
			denialReason = "painting is unavailable"
		}
		return "", fmt.Errorf("%s", denialReason)
	}

	content, _ := response.StructuredResult["content"].(string)
	return content, nil
}

func validatePaintSaveRequest(rawRequest PaintSaveRequest) (PaintSaveRequest, error) {
	validatedRequest := PaintSaveRequest{
		Title:      normalizePaintTitle(rawRequest.Title),
		Width:      rawRequest.Width,
		Height:     rawRequest.Height,
		Background: strings.ToUpper(strings.TrimSpace(rawRequest.Background)),
		Strokes:    make([]PaintStroke, 0, len(rawRequest.Strokes)),
	}
	if validatedRequest.Width == 0 {
		validatedRequest.Width = defaultPaintCanvasWidth
	}
	if validatedRequest.Height == 0 {
		validatedRequest.Height = defaultPaintCanvasHeight
	}
	if validatedRequest.Width < minPaintCanvasWidth || validatedRequest.Width > maxPaintCanvasDimension {
		return PaintSaveRequest{}, fmt.Errorf("paint width must be between %d and %d", minPaintCanvasWidth, maxPaintCanvasDimension)
	}
	if validatedRequest.Height < minPaintCanvasHeight || validatedRequest.Height > maxPaintCanvasDimension {
		return PaintSaveRequest{}, fmt.Errorf("paint height must be between %d and %d", minPaintCanvasHeight, maxPaintCanvasDimension)
	}
	if validatedRequest.Background == "" {
		validatedRequest.Background = defaultPaintBackground
	}
	if !paintHexColorPattern.MatchString(validatedRequest.Background) {
		return PaintSaveRequest{}, fmt.Errorf("paint background must be a hex color")
	}
	if len(rawRequest.Strokes) == 0 {
		return PaintSaveRequest{}, fmt.Errorf("at least one stroke is required")
	}
	if len(rawRequest.Strokes) > maxPaintStrokes {
		return PaintSaveRequest{}, fmt.Errorf("paint request exceeds maximum stroke count")
	}

	totalPointCount := 0
	for strokeIndex, rawStroke := range rawRequest.Strokes {
		validatedStroke, validatedPointCount, err := validatePaintStroke(rawStroke, validatedRequest.Width, validatedRequest.Height)
		if err != nil {
			return PaintSaveRequest{}, fmt.Errorf("stroke %d: %w", strokeIndex+1, err)
		}
		totalPointCount += validatedPointCount
		if totalPointCount > maxPaintTotalPoints {
			return PaintSaveRequest{}, fmt.Errorf("paint request exceeds maximum point count")
		}
		validatedRequest.Strokes = append(validatedRequest.Strokes, validatedStroke)
	}

	return validatedRequest, nil
}

func validatePaintStroke(rawStroke PaintStroke, canvasWidth int, canvasHeight int) (PaintStroke, int, error) {
	validatedStroke := PaintStroke{
		Color:  strings.ToUpper(strings.TrimSpace(rawStroke.Color)),
		Width:  rawStroke.Width,
		Points: make([]PaintPoint, 0, len(rawStroke.Points)),
	}
	if validatedStroke.Color == "" {
		validatedStroke.Color = defaultPaintStrokeColor
	}
	if !paintHexColorPattern.MatchString(validatedStroke.Color) {
		return PaintStroke{}, 0, fmt.Errorf("stroke color must be a hex color")
	}
	if math.IsNaN(validatedStroke.Width) || math.IsInf(validatedStroke.Width, 0) || validatedStroke.Width < 1 || validatedStroke.Width > 48 {
		return PaintStroke{}, 0, fmt.Errorf("stroke width must be between 1 and 48")
	}
	if len(rawStroke.Points) == 0 {
		return PaintStroke{}, 0, fmt.Errorf("stroke must contain at least one point")
	}
	if len(rawStroke.Points) > maxPaintPointsPerStroke {
		return PaintStroke{}, 0, fmt.Errorf("stroke exceeds maximum point count")
	}

	maxX := float64(canvasWidth)
	maxY := float64(canvasHeight)
	for pointIndex, rawPoint := range rawStroke.Points {
		if !isFinitePaintCoordinate(rawPoint.X) || !isFinitePaintCoordinate(rawPoint.Y) {
			return PaintStroke{}, 0, fmt.Errorf("point %d contains a non-finite coordinate", pointIndex+1)
		}
		if rawPoint.X < 0 || rawPoint.X > maxX || rawPoint.Y < 0 || rawPoint.Y > maxY {
			return PaintStroke{}, 0, fmt.Errorf("point %d is outside the canvas", pointIndex+1)
		}
		validatedStroke.Points = append(validatedStroke.Points, PaintPoint{X: rawPoint.X, Y: rawPoint.Y})
	}

	return validatedStroke, len(validatedStroke.Points), nil
}

func buildPaintSVG(validatedRequest PaintSaveRequest) (string, error) {
	var svgBuilder strings.Builder
	svgBuilder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	svgBuilder.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `)
	svgBuilder.WriteString(strconv.Itoa(validatedRequest.Width))
	svgBuilder.WriteByte(' ')
	svgBuilder.WriteString(strconv.Itoa(validatedRequest.Height))
	svgBuilder.WriteString(`" width="`)
	svgBuilder.WriteString(strconv.Itoa(validatedRequest.Width))
	svgBuilder.WriteString(`" height="`)
	svgBuilder.WriteString(strconv.Itoa(validatedRequest.Height))
	svgBuilder.WriteString(`" role="img">` + "\n<title>")
	if err := xml.EscapeText(&svgBuilder, []byte(validatedRequest.Title)); err != nil {
		return "", fmt.Errorf("escape painting title: %w", err)
	}
	svgBuilder.WriteString("</title>\n<desc>Created in Haven Paint Lite</desc>\n")
	svgBuilder.WriteString(`<rect width="100%" height="100%" fill="`)
	svgBuilder.WriteString(validatedRequest.Background)
	svgBuilder.WriteString(`"/>` + "\n")

	for _, stroke := range validatedRequest.Strokes {
		if len(stroke.Points) == 1 {
			svgBuilder.WriteString(`<circle cx="`)
			svgBuilder.WriteString(formatPaintFloat(stroke.Points[0].X))
			svgBuilder.WriteString(`" cy="`)
			svgBuilder.WriteString(formatPaintFloat(stroke.Points[0].Y))
			svgBuilder.WriteString(`" r="`)
			svgBuilder.WriteString(formatPaintFloat(stroke.Width / 2))
			svgBuilder.WriteString(`" fill="`)
			svgBuilder.WriteString(stroke.Color)
			svgBuilder.WriteString(`"/>` + "\n")
			continue
		}

		svgBuilder.WriteString(`<path d="`)
		svgBuilder.WriteString(buildPaintPathData(stroke.Points))
		svgBuilder.WriteString(`" fill="none" stroke="`)
		svgBuilder.WriteString(stroke.Color)
		svgBuilder.WriteString(`" stroke-width="`)
		svgBuilder.WriteString(formatPaintFloat(stroke.Width))
		svgBuilder.WriteString(`" stroke-linecap="round" stroke-linejoin="round"/>` + "\n")
	}

	svgBuilder.WriteString(`</svg>` + "\n")
	return svgBuilder.String(), nil
}

func buildPaintPathData(points []PaintPoint) string {
	var pathBuilder strings.Builder
	pathBuilder.WriteString("M ")
	pathBuilder.WriteString(formatPaintFloat(points[0].X))
	pathBuilder.WriteByte(' ')
	pathBuilder.WriteString(formatPaintFloat(points[0].Y))
	for _, point := range points[1:] {
		pathBuilder.WriteString(" L ")
		pathBuilder.WriteString(formatPaintFloat(point.X))
		pathBuilder.WriteByte(' ')
		pathBuilder.WriteString(formatPaintFloat(point.Y))
	}
	return pathBuilder.String()
}

func buildPaintFilename(title string, createdAt time.Time) string {
	slug := slugifyPaintTitle(title)
	if slug == "" {
		slug = "untitled-painting"
	}
	return fmt.Sprintf("%s-%04d-%s.svg", createdAt.Format("20060102-150405"), createdAt.UnixNano()%10000, slug)
}

func normalizePaintTitle(rawTitle string) string {
	trimmedTitle := strings.TrimSpace(rawTitle)
	if trimmedTitle == "" {
		return "Untitled Painting"
	}
	if len(trimmedTitle) > maxPaintTitleLength {
		return trimmedTitle[:maxPaintTitleLength]
	}
	return trimmedTitle
}

func slugifyPaintTitle(title string) string {
	normalizedTitle := strings.ToLower(strings.TrimSpace(title))
	if normalizedTitle == "" {
		return ""
	}

	var slugBuilder strings.Builder
	lastWasSeparator := false
	for _, titleRune := range normalizedTitle {
		switch {
		case unicode.IsLetter(titleRune) || unicode.IsNumber(titleRune):
			slugBuilder.WriteRune(titleRune)
			lastWasSeparator = false
		case unicode.IsSpace(titleRune) || titleRune == '-' || titleRune == '_':
			if !lastWasSeparator && slugBuilder.Len() > 0 {
				slugBuilder.WriteRune('-')
				lastWasSeparator = true
			}
		}
	}

	return strings.Trim(slugBuilder.String(), "-")
}

func paintTitleFromFilename(filename string) string {
	baseName := strings.TrimSuffix(filename, filepath.Ext(filename))
	if len(baseName) > 20 && baseName[8] == '-' && baseName[15] == '-' {
		baseName = baseName[20:]
	}
	baseName = strings.ReplaceAll(baseName, "-", " ")
	baseName = strings.ReplaceAll(baseName, "_", " ")
	return titleCasePaintWords(strings.TrimSpace(baseName))
}

func paintTitleFromSVG(svgContent string, fallbackTitle string) string {
	startIndex := strings.Index(svgContent, "<title>")
	endIndex := strings.Index(svgContent, "</title>")
	if startIndex == -1 || endIndex == -1 || endIndex <= startIndex+len("<title>") {
		return fallbackTitle
	}
	return html.UnescapeString(strings.TrimSpace(svgContent[startIndex+len("<title>") : endIndex]))
}

func formatPaintFloat(value float64) string {
	formattedValue := strconv.FormatFloat(value, 'f', 2, 64)
	formattedValue = strings.TrimRight(formattedValue, "0")
	formattedValue = strings.TrimRight(formattedValue, ".")
	if formattedValue == "" || formattedValue == "-0" {
		return "0"
	}
	return formattedValue
}

func isFinitePaintCoordinate(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isMissingPaintGalleryError(err error) bool {
	lowerError := strings.ToLower(err.Error())
	return strings.Contains(lowerError, "not found") || strings.Contains(lowerError, "no such file") || strings.Contains(lowerError, "does not exist")
}

func titleCasePaintWords(value string) string {
	if value == "" {
		return ""
	}

	var titleBuilder strings.Builder
	makeNextUpper := true
	for _, valueRune := range value {
		switch {
		case unicode.IsLetter(valueRune) || unicode.IsNumber(valueRune):
			if makeNextUpper {
				titleBuilder.WriteRune(unicode.ToUpper(valueRune))
				makeNextUpper = false
			} else {
				titleBuilder.WriteRune(valueRune)
			}
		default:
			titleBuilder.WriteRune(valueRune)
			makeNextUpper = true
		}
	}
	return titleBuilder.String()
}
