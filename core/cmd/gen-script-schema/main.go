// Command gen-script-schema regenerates the checked-in JSON Schema
// for ScriptDefinition from the Go structs in internal/script. Run it
// after any change to internal/script's step or definition types:
//
//	go run ./cmd/gen-script-schema -out internal/script/atomyx-script.schema.json
//
// With no -out flag it writes the schema to stdout instead.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"dev.atomyx/internal/script"
)

func main() {
	out := flag.String("out", "", "output file path; defaults to stdout")
	flag.Parse()

	data, err := json.MarshalIndent(script.GenerateJSONSchema(), "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-script-schema: encoding schema: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')

	if *out == "" {
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintf(os.Stderr, "gen-script-schema: writing stdout: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gen-script-schema: writing %s: %v\n", *out, err)
		os.Exit(1)
	}
}
