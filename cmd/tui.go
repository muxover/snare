package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	"github.com/muxover/snare/intercept"
	"github.com/muxover/snare/mock"
	"github.com/muxover/snare/tui"
	"github.com/spf13/cobra"
)

var tuiProxy string

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Interactive terminal UI for browsing captures",
	Long:  "Opens a live-updating terminal UI. Browse, inspect, and replay captures with keyboard navigation. Polls the store directory every 2 s for new captures.",
	RunE:  runTUI,
}

func init() {
	tuiCmd.Flags().StringVar(&tuiProxy, "proxy", "http://127.0.0.1:8888", "Proxy URL to route replays through (set to empty to skip capturing)")
}

func runTUI(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	mocks := mock.NewStore(config.MockFile())
	iq := intercept.NewQueue(config.InterceptDir())
	m := tui.New(store, mocks, iq, tuiProxy)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
