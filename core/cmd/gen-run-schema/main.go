// Command gen-run-schema regenerates the checked-in JSON Schema for
// the run folder meta.json from the Go types in internal/runfolder.
// Run it after any change to those types:
//
//	go run ./cmd/gen-run-schema -out internal/runfolder/atomyx-run.schema.json
//
// With no -out flag it writes the schema to stdout instead.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"dev.atomyx/internal/runfolder"
)

func main() {
	out := flag.String("out", "", "output file path; defaults to stdout")
	flag.Parse()

	data, err := json.MarshalIndent(runfolder.GenerateJSONSchema(), "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-run-schema: encoding schema: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')

	if *out == "" {
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintf(os.Stderr, "gen-run-schema: writing stdout: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gen-run-schema: writing %s: %v\n", *out, err)
		os.Exit(1)
	}
}
