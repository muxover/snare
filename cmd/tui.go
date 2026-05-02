package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"github.com/muxover/snare/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Interactive terminal UI for browsing captures",
	Long:  "Opens a live-updating terminal UI. Browse, inspect, and replay captures with keyboard navigation. Polls the store directory every 2 s for new captures.",
	RunE:  runTUI,
}

func runTUI(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	m := tui.New(store)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
