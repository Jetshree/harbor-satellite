package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	outputFlag string
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "groundctl",
		Short: "groundctl is the administrative command-line client for Harbor Satellite Ground Control",
		Long: `groundctl allows administrators to manage edge satellites, groups, configurations, 
and inspect operational statuses directly from the terminal without manual REST API scripting.`,
	}

	rootCmd.PersistentFlags().StringVarP(&outputFlag, "output", "o", "table", "Output format [table|json|yaml]")

	rootCmd.AddCommand(LoginCmd())
	rootCmd.AddCommand(LogoutCmd())
	rootCmd.AddCommand(WhoamiCmd())
	rootCmd.AddCommand(SatelliteCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
