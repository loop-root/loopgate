package tools

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	paintDirectoryRelativePath  = "outputs/paintings"
	defaultPaintCanvasWidth     = 960
	defaultPaintCanvasHeight    = 540
	defaultPaintBackground      = "#F6F1E7"
	defaultPaintStrokeColor     = "#3F2D1F"
	maxPaintTitleLength         = 80
	maxPaintStrokes             = 96
	maxPaintPointsPerStroke     = 512
	maxPaintTotalPoints         = 8192
	minPaintCanvasWidth         = 320
	minPaintCanvasHeight        = 240
	maxPaintCanvasDimension     = 2048
	maxPaintStrokePayloadLength = 65536
)

var paintHexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type paintPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type paintStroke struct {
	Color  string       `json:"color"`
	Width  float64      `json:"width"`
	Points []paintPoint `json:"points"`
}

type paintSaveRequest struct {
	Title      string        `json:"title"`
	Width      int           `json:"width"`
	Height     int           `json:"height"`
	Background string        `json:"background"`
	Strokes    []paintStroke `json:"strokes"`
}

// PaintList lists recent paintings created inside Haven.
type PaintList struct {
	Root string
}

func (tool *PaintList) Name() string      { return "paint.list" }
func (tool *PaintList) Category() string  { return "filesystem" }
func (tool *PaintList) Operation() string { return OpRead }

func (tool *PaintList) Schema() Schema {
	return Schema{
		Description: "List paintings in Morph's paint gallery.",
	}
}

func (tool *PaintList) Execute(context.Context, map[string]string) (string, error) {
	paintDirectoryPath := filepath.Join(tool.Root, filepath.FromSlash(paintDirectoryRelativePath))
	directoryEntries, err := os.ReadDir(paintDirectoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "No paintings yet.", nil
		}
		return "", fmt.Errorf("list paintings: %w", err)
	}

	type paintEntry struct {
		name       string
		modifiedAt time.Time
	}
	paintEntries := make([]paintEntry, 0, len(directoryEntries))
	for _, directoryEntry := range directoryEntries {
		if directoryEntry.IsDir() || !strings.HasSuffix(strings.ToLower(directoryEntry.Name()), ".svg") {
			continue
		}
		info, infoErr := directoryEntry.Info()
		if infoErr != nil {
			continue
		}
		paintEntries = append(paintEntries, paintEntry{
			name:       directoryEntry.Name(),
			modifiedAt: info.ModTime().UTC(),
		})
	}
	if len(paintEntries) == 0 {
		return "No paintings yet.", nil
	}

	sort.Slice(paintEntries, func(leftIndex, rightIndex int) bool {
		return paintEntries[leftIndex].modifiedAt.After(paintEntries[rightIndex].modifiedAt)
	})

	var builder strings.Builder
	builder.WriteString("Paint gallery:\n")
	for _, paintEntry := range paintEntries {
		builder.WriteString("- ")
		builder.WriteString(filepath.ToSlash(filepath.Join("artifacts", "paintings", paintEntry.name)))
		builder.WriteString(" (updated ")
		builder.WriteString(paintEntry.modifiedAt.Format(time.RFC3339))
		builder.WriteString(")\n")
	}
	return strings.TrimSpace(builder.String()), nil
}

// PaintSave validates stroke coordinates and saves a structured SVG painting.
type PaintSave struct {
	Root string
}

func (tool *PaintSave) Name() string      { return "paint.save" }
func (tool *PaintSave) Category() string  { return "filesystem" }
func (tool *PaintSave) Operation() string { return OpWrite }

func (tool *PaintSave) Schema() Schema {
	return Schema{
		Description: "Create and save a painting in Haven using explicit stroke coordinates. Provide a short title plus strokes_json as a JSON array where each stroke has a hex color, numeric width, and a points array of {x,y} coordinates. Optionally set canvas width, canvas height, and background.",
		Args: []ArgDef{
			{
				Name:        "title",
				Description: "Short title for the painting",
				Required:    true,
				Type:        "string",
				MaxLen:      maxPaintTitleLength,
			},
			{
				Name:        "strokes_json",
				Description: "JSON array of stroke objects. Example: [{\"color\":\"#8E6C4B\",\"width\":6,\"points\":[{\"x\":120,\"y\":160},{\"x\":320,\"y\":190}]}]",
				Required:    true,
				Type:        "string",
				MaxLen:      maxPaintStrokePayloadLength,
			},
			{
				Name:        "width",
				Description: "Optional canvas width in pixels (default 960)",
				Required:    false,
				Type:        "int",
				MaxLen:      5,
			},
			{
				Name:        "height",
				Description: "Optional canvas height in pixels (default 540)",
				Required:    false,
				Type:        "int",
				MaxLen:      5,
			},
			{
				Name:        "background",
				Description: "Optional canvas background color as a hex value like #F6F1E7",
				Required:    false,
				Type:        "string",
				MaxLen:      7,
			},
		},
	}
}

func (tool *PaintSave) Execute(_ context.Context, args map[string]string) (string, error) {
	rawWidth := strings.TrimSpace(args["width"])
	rawHeight := strings.TrimSpace(args["height"])
	validatedWidth, err := parseOptionalPaintDimension(rawWidth, "width")
	if err != nil {
		return "", err
	}
	validatedHeight, err := parseOptionalPaintDimension(rawHeight, "height")
	if err != nil {
		return "", err
	}

	parsedStrokes, err := parsePaintStrokesJSON(strings.TrimSpace(args["strokes_json"]))
	if err != nil {
		return "", err
	}

	validatedRequest, err := validatePaintSaveRequest(paintSaveRequest{
		Title:      strings.TrimSpace(args["title"]),
		Width:      validatedWidth,
		Height:     validatedHeight,
		Background: strings.TrimSpace(args["background"]),
		Strokes:    parsedStrokes,
	})
	if err != nil {
		return "", err
	}

	paintDirectoryPath := filepath.Join(tool.Root, filepath.FromSlash(paintDirectoryRelativePath))
	if err := os.MkdirAll(paintDirectoryPath, 0o700); err != nil {
		return "", fmt.Errorf("create paint directory: %w", err)
	}

	nowUTC := time.Now().UTC()
	fileName := buildPaintFilename(validatedRequest.Title, nowUTC)
	paintingPath := filepath.Join(paintDirectoryPath, fileName)
	svgContent, err := buildPaintSVG(validatedRequest)
	if err != nil {
		return "", fmt.Errorf("build painting: %w", err)
	}

	if err := os.WriteFile(paintingPath, []byte(svgContent), 0o600); err != nil {
		return "", fmt.Errorf("save painting: %w", err)
	}
	return fmt.Sprintf("Painting saved to %s", filepath.ToSlash(filepath.Join("artifacts", "paintings", fileName))), nil
}

func parseOptionalPaintDimension(rawValue string, dimensionName string) (int, error) {
	if rawValue == "" {
		return 0, nil
	}
	parsedValue, err := strconv.Atoi(rawValue)
	if err != nil {
		return 0, fmt.Errorf("paint %s must be an integer", dimensionName)
	}
	return parsedValue, nil
}

func parsePaintStrokesJSON(rawJSON string) ([]paintStroke, error) {
	if rawJSON == "" {
		return nil, fmt.Errorf("paint strokes_json is required")
	}

	decoder := json.NewDecoder(strings.NewReader(rawJSON))
	decoder.DisallowUnknownFields()

	var parsedStrokes []paintStroke
	if err := decoder.Decode(&parsedStrokes); err != nil {
		return nil, fmt.Errorf("invalid strokes_json: %w", err)
	}

	var trailingValue interface{}
	if err := decoder.Decode(&trailingValue); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("invalid strokes_json: unexpected trailing content")
		}
		return nil, fmt.Errorf("invalid strokes_json: %w", err)
	}
	return parsedStrokes, nil
}

func validatePaintSaveRequest(rawRequest paintSaveRequest) (paintSaveRequest, error) {
	validatedRequest := paintSaveRequest{
		Title:      normalizePaintTitle(rawRequest.Title),
		Width:      rawRequest.Width,
		Height:     rawRequest.Height,
		Background: strings.ToUpper(strings.TrimSpace(rawRequest.Background)),
		Strokes:    make([]paintStroke, 0, len(rawRequest.Strokes)),
	}
	if validatedRequest.Width == 0 {
		validatedRequest.Width = defaultPaintCanvasWidth
	}
	if validatedRequest.Height == 0 {
		validatedRequest.Height = defaultPaintCanvasHeight
	}
	if validatedRequest.Width < minPaintCanvasWidth || validatedRequest.Width > maxPaintCanvasDimension {
		return paintSaveRequest{}, fmt.Errorf("paint width must be between %d and %d", minPaintCanvasWidth, maxPaintCanvasDimension)
	}
	if validatedRequest.Height < minPaintCanvasHeight || validatedRequest.Height > maxPaintCanvasDimension {
		return paintSaveRequest{}, fmt.Errorf("paint height must be between %d and %d", minPaintCanvasHeight, maxPaintCanvasDimension)
	}
	if validatedRequest.Background == "" {
		validatedRequest.Background = defaultPaintBackground
	}
	if !paintHexColorPattern.MatchString(validatedRequest.Background) {
		return paintSaveRequest{}, fmt.Errorf("paint background must be a hex color")
	}
	if len(rawRequest.Strokes) == 0 {
		return paintSaveRequest{}, fmt.Errorf("at least one stroke is required")
	}
	if len(rawRequest.Strokes) > maxPaintStrokes {
		return paintSaveRequest{}, fmt.Errorf("paint request exceeds maximum stroke count")
	}

	totalPointCount := 0
	for strokeIndex, rawStroke := range rawRequest.Strokes {
		validatedStroke, validatedPointCount, err := validatePaintStroke(rawStroke, validatedRequest.Width, validatedRequest.Height)
		if err != nil {
			return paintSaveRequest{}, fmt.Errorf("stroke %d: %w", strokeIndex+1, err)
		}
		totalPointCount += validatedPointCount
		if totalPointCount > maxPaintTotalPoints {
			return paintSaveRequest{}, fmt.Errorf("paint request exceeds maximum point count")
		}
		validatedRequest.Strokes = append(validatedRequest.Strokes, validatedStroke)
	}

	return validatedRequest, nil
}

func validatePaintStroke(rawStroke paintStroke, canvasWidth int, canvasHeight int) (paintStroke, int, error) {
	validatedStroke := paintStroke{
		Color:  strings.ToUpper(strings.TrimSpace(rawStroke.Color)),
		Width:  rawStroke.Width,
		Points: make([]paintPoint, 0, len(rawStroke.Points)),
	}
	if validatedStroke.Color == "" {
		validatedStroke.Color = defaultPaintStrokeColor
	}
	if !paintHexColorPattern.MatchString(validatedStroke.Color) {
		return paintStroke{}, 0, fmt.Errorf("stroke color must be a hex color")
	}
	if math.IsNaN(validatedStroke.Width) || math.IsInf(validatedStroke.Width, 0) || validatedStroke.Width < 1 || validatedStroke.Width > 48 {
		return paintStroke{}, 0, fmt.Errorf("stroke width must be between 1 and 48")
	}
	if len(rawStroke.Points) == 0 {
		return paintStroke{}, 0, fmt.Errorf("stroke must contain at least one point")
	}
	if len(rawStroke.Points) > maxPaintPointsPerStroke {
		return paintStroke{}, 0, fmt.Errorf("stroke exceeds maximum point count")
	}

	maxX := float64(canvasWidth)
	maxY := float64(canvasHeight)
	for pointIndex, rawPoint := range rawStroke.Points {
		if !isFinitePaintCoordinate(rawPoint.X) || !isFinitePaintCoordinate(rawPoint.Y) {
			return paintStroke{}, 0, fmt.Errorf("point %d contains a non-finite coordinate", pointIndex+1)
		}
		if rawPoint.X < 0 || rawPoint.X > maxX || rawPoint.Y < 0 || rawPoint.Y > maxY {
			return paintStroke{}, 0, fmt.Errorf("point %d is outside the canvas", pointIndex+1)
		}
		validatedStroke.Points = append(validatedStroke.Points, paintPoint{X: rawPoint.X, Y: rawPoint.Y})
	}

	return validatedStroke, len(validatedStroke.Points), nil
}

func buildPaintSVG(validatedRequest paintSaveRequest) (string, error) {
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
	svgBuilder.WriteString("</title>\n<desc>Created in Haven Paint</desc>\n")
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

func buildPaintPathData(points []paintPoint) string {
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

func slugifyPaintTitle(rawTitle string) string {
	trimmedTitle := strings.TrimSpace(strings.ToLower(rawTitle))
	if trimmedTitle == "" {
		return "painting"
	}

	var builder strings.Builder
	lastWasDash := false
	for _, currentRune := range trimmedTitle {
		switch {
		case unicode.IsLetter(currentRune) || unicode.IsNumber(currentRune):
			builder.WriteRune(currentRune)
			lastWasDash = false
		default:
			if !lastWasDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastWasDash = true
			}
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "painting"
	}
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
	}
	if slug == "" {
		return "painting"
	}
	return slug
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
