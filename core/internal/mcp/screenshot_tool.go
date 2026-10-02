package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var screenshotInputSchema = jsEmptyObject()

var screenshotSchema = compileToolSchema("screenshot", screenshotInputSchema)

const (
	screenshotDirName  = ".atomyx"
	screenshotSubDir   = "screenshots"
	screenshotDirMode  = 0o755
	screenshotFileMode = 0o644
)

// detectScreenshotExt reports the file extension for bytes' image
// format, sniffed from its leading magic bytes: JPEG starts with
// 0xFF 0xD8, everything else screenshot capture produces is PNG.
func detectScreenshotExt(bytes []byte) string {
	if len(bytes) >= 2 && bytes[0] == 0xff && bytes[1] == 0xd8 {
		return "jpg"
	}
	return "png"
}

type screenshotResult struct {
	Path      string `json:"path"`
	SizeBytes int    `json:"sizeBytes"`
	Format    string `json:"format"`
}

// registerScreenshotTool adds the screenshot tool: captures the
// active device's screen and saves it under .atomyx/screenshots in
// the working directory, returning the file path for the caller to
// read directly.
func registerScreenshotTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "screenshot",
		Description: "Use this only as a last resort, when get_ui_tree cannot answer your question " +
			"(e.g. custom-rendered canvas, Flutter without semantics, visual " +
			"layout validation, or when you suspect the UI tree is missing " +
			"elements). Saves a compressed JPEG to .atomyx/screenshots/ and " +
			"returns the file path. Read the file to view. Always try " +
			"get_ui_tree or find_element FIRST because they are faster, cheaper, " +
			"and give you actionable selectors directly.",
		InputSchema: screenshotInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(screenshotSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("screenshot", err), nil
		}

		orch, err := orchestraOrFail(ctx, tc)
		if err != nil {
			return errorResult(err), nil
		}
		data, err := orch.Screenshot(ctx)
		if err != nil {
			return errorResult(err), nil
		}
		ext := detectScreenshotExt(data)
		cwd, err := os.Getwd()
		if err != nil {
			return errorResult(err), nil
		}
		dir := filepath.Join(cwd, screenshotDirName, screenshotSubDir)
		if err := os.MkdirAll(dir, screenshotDirMode); err != nil {
			return errorResult(err), nil
		}
		filename := fmt.Sprintf("screenshot-%d.%s", tc.Clock.NowMs(), ext)
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, data, screenshotFileMode); err != nil {
			return errorResult(err), nil
		}
		return jsonToolResult(screenshotResult{Path: path, SizeBytes: len(data), Format: ext})
	})
}
