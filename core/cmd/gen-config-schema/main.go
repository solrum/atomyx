// Command gen-config-schema regenerates the checked-in JSON Schema
// for AtomyxConfig from the Go types in internal/config. Run it after
// any change to internal/config's field or schema-generation types:
//
//	go run ./cmd/gen-config-schema -out internal/config/atomyx-config.schema.json
//
// With no -out flag it writes the schema to stdout instead.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"dev.atomyx/internal/config"
)

func main() {
	out := flag.String("out", "", "output file path; defaults to stdout")
	flag.Parse()

	data, err := json.MarshalIndent(config.GenerateJSONSchema(), "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-config-schema: encoding schema: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')

	if *out == "" {
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintf(os.Stderr, "gen-config-schema: writing stdout: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "gen-config-schema: writing %s: %v\n", *out, err)
		os.Exit(1)
	}
}
