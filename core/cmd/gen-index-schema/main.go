// Command gen-index-schema regenerates the checked-in JSON Schema for
// the suite run folder index.json from the Go types in internal/runfolder.
// Run it after any change to those types:
//
//	go run ./cmd/gen-index-schema -out internal/runfolder/atomyx-run-index.schema.json
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

	data, err := json.MarshalIndent(runfolder.GenerateIndexJSONSchema(), "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-index-schema: encoding schema: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')

	if *out == "" {
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintf(os.Stderr, "gen-index-schema: writing stdout: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gen-index-schema: writing %s: %v\n", *out, err)
		os.Exit(1)
	}
}
