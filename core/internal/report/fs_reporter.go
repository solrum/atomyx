package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"dev.atomyx/internal/runfolder"
)

// Reporter turns a run's in-memory result into a persisted report
// report. Implementations decide the storage backend; callers only
// see the report directory and the files written.
type Reporter interface {
	Generate(input ReportInput, options ReportOptions) (ReportBundle, error)
}

type fsReporter struct{}

// NewFsReporter returns a Reporter that writes every screenshot into
// options.ArtifactsDir, named to match the relative paths the JSON
// renderer references, and writes report.html, report.json and junit.xml into
// options.ReportDir per the requested formats. HTML image links lead
// from ReportDir to ArtifactsDir. A Reporter given no formats writes the
// screenshots only and never creates ReportDir.
func NewFsReporter() Reporter {
	return fsReporter{}
}

func (fsReporter) Generate(input ReportInput, options ReportOptions) (ReportBundle, error) {
	if options.ArtifactsDir == "" {
		return ReportBundle{}, errors.New("report: ReportOptions.ArtifactsDir is required")
	}
	wantsReport := len(options.Formats) > 0
	if wantsReport && options.ReportDir == "" {
		return ReportBundle{}, errors.New("report: ReportOptions.ReportDir is required when a format is requested")
	}

	var files []string

	screenshots := input.ReportScreenshots()
	index := BuildScreenshotIndex(screenshots)
	if len(index.Refs) > 0 {
		if err := os.MkdirAll(options.ArtifactsDir, 0o755); err != nil {
			return ReportBundle{}, fmt.Errorf("report: creating artifacts directory: %w", err)
		}
		for i, screenshot := range screenshots {
			filePath := filepath.Join(options.ArtifactsDir, index.Refs[i].File)
			if err := os.WriteFile(filePath, screenshot.Data, 0o644); err != nil {
				return ReportBundle{}, fmt.Errorf("report: writing screenshot %q: %w", index.Refs[i].File, err)
			}
			files = append(files, filePath)
		}
	}

	if wantsReport {
		if err := os.MkdirAll(options.ReportDir, 0o755); err != nil {
			return ReportBundle{}, fmt.Errorf("report: creating report directory: %w", err)
		}
	}

	if slices.Contains(options.Formats, FormatHTML) {
		imageBase, err := ArtifactsLink(options.ReportDir, options.ArtifactsDir)
		if err != nil {
			return ReportBundle{}, err
		}
		html, err := RenderHTML(input, index.ByScope, imageBase)
		if err != nil {
			return ReportBundle{}, err
		}
		htmlPath := filepath.Join(options.ReportDir, "report.html")
		if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
			return ReportBundle{}, fmt.Errorf("report: writing report.html: %w", err)
		}
		files = append(files, htmlPath)
	}

	if slices.Contains(options.Formats, FormatJSON) {
		rendered, err := RenderJSON(input, index.ByScope)
		if err != nil {
			return ReportBundle{}, err
		}
		encoded, err := json.MarshalIndent(rendered, "", "  ")
		if err != nil {
			return ReportBundle{}, fmt.Errorf("report: encoding report.json: %w", err)
		}
		jsonPath := filepath.Join(options.ReportDir, "report.json")
		if err := os.WriteFile(jsonPath, append(encoded, '\n'), 0o644); err != nil {
			return ReportBundle{}, fmt.Errorf("report: writing report.json: %w", err)
		}
		files = append(files, jsonPath)
	}

	if slices.Contains(options.Formats, FormatJUnit) {
		rendered, err := RenderJUnit(input)
		if err != nil {
			return ReportBundle{}, err
		}
		junitPath := filepath.Join(options.ReportDir, runfolder.JUnitFileName)
		if err := os.WriteFile(junitPath, rendered, 0o644); err != nil {
			return ReportBundle{}, fmt.Errorf("report: writing %s: %w", runfolder.JUnitFileName, err)
		}
		files = append(files, junitPath)
	}

	return ReportBundle{BundleDir: options.ReportDir, Files: files}, nil
}
