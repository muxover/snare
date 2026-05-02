package cmd

import (
	"github.com/spf13/cobra"
)

const Version = "1.6.0"

var rootCmd = &cobra.Command{
	Use:   "snare",
	Short: "HTTP/HTTPS proxy that intercepts and captures requests",
	Long:  "Snare is a CLI that runs a local HTTP/HTTPS proxy to intercept, capture, save, and replay traffic. Point HTTP_PROXY/HTTPS_PROXY at it and inspect or replay every request.",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("snare version {{.Version}}\n")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(replayCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(mockCmd)
	rootCmd.AddCommand(interceptCmd)
	rootCmd.AddCommand(tuiCmd)
	rootCmd.AddCommand(pipeCmd)
	rootCmd.AddCommand(assertCmd)
	rootCmd.AddCommand(saveCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(clearCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(caCmd)
}
