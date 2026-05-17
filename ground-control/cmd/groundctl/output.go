package main

import (
	"encoding/json"
	"io"

	"gopkg.in/yaml.v3"
)

// PrintResult formats and prints data to the specified output stream in table, json, or yaml format.
func PrintResult(w io.Writer, format string, data interface{}, tablePrinter func()) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	case "yaml":
		enc := yaml.NewEncoder(w)
		return enc.Encode(data)
	default: // table
		if tablePrinter != nil {
			tablePrinter()
			return nil
		}
		// Fallback to YAML if table printer is not provided
		enc := yaml.NewEncoder(w)
		return enc.Encode(data)
	}
}
