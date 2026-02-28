package cmd

import (
	"github.com/spf13/cobra"
)

const Version = "0.1.0"

var rootCmd = &cobra.Command{
	Use:   "snare",
	Short: "HTTP/HTTPS proxy that intercepts and captures requests",
	Long:  "Snare is a CLI that runs a local HTTP/HTTPS proxy to intercept, capture, save, and replay traffic. Point HTTP_PROXY/HTTPS_PROXY at it and inspect or replay every request.",
}

// Execute runs the root command and returns any error (e.g. from subcommands).
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("snare version {{.Version}}\n")
}

func init() {
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(replayCmd)
	rootCmd.AddCommand(saveCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(clearCmd)
	rootCmd.AddCommand(caCmd)
}
