package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Reporter turns a run's in-memory result into a persisted report
// bundle. Implementations decide the storage backend; callers only
// see the bundle directory and the files it contains.
type Reporter interface {
	Generate(input ReportInput, options ReportOptions) (ReportBundle, error)
}

type fsReporter struct{}

// NewFsReporter returns a Reporter that writes a report bundle under
// <outDir>/<runId>/: report.html and report.json per the requested
// formats, and every screenshot under a screenshots/ subdirectory,
// named to match the relative paths the HTML and JSON renderers
// reference.
func NewFsReporter() Reporter {
	return fsReporter{}
}

func (fsReporter) Generate(input ReportInput, options ReportOptions) (ReportBundle, error) {
	bundleDir := filepath.Join(options.OutDir, input.ReportMetadata().RunID)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return ReportBundle{}, fmt.Errorf("report: creating bundle directory: %w", err)
	}

	var files []string

	screenshots := input.ReportScreenshots()
	index := BuildScreenshotIndex(screenshots)
	if len(index.Refs) > 0 {
		screenshotsDir := filepath.Join(bundleDir, "screenshots")
		if err := os.MkdirAll(screenshotsDir, 0o755); err != nil {
			return ReportBundle{}, fmt.Errorf("report: creating screenshots directory: %w", err)
		}
		for i, screenshot := range screenshots {
			filePath := filepath.Join(screenshotsDir, index.Refs[i].File)
			if err := os.WriteFile(filePath, screenshot.Data, 0o644); err != nil {
				return ReportBundle{}, fmt.Errorf("report: writing screenshot %q: %w", index.Refs[i].File, err)
			}
			files = append(files, filePath)
		}
	}

	if slices.Contains(options.Formats, FormatHTML) {
		html, err := RenderHTML(input, index.ByScope)
		if err != nil {
			return ReportBundle{}, err
		}
		htmlPath := filepath.Join(bundleDir, "report.html")
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
		jsonPath := filepath.Join(bundleDir, "report.json")
		if err := os.WriteFile(jsonPath, append(encoded, '\n'), 0o644); err != nil {
			return ReportBundle{}, fmt.Errorf("report: writing report.json: %w", err)
		}
		files = append(files, jsonPath)
	}

	return ReportBundle{BundleDir: bundleDir, Files: files}, nil
}
