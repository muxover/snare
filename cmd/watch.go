package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"

	"github.com/spf13/cobra"
)

var watchPoll time.Duration

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Print new captures as they are saved",
	Long:  "Poll the capture store and print one line per new capture (same columns as list). Use Ctrl+C to stop.",
	RunE:  runWatch,
}

func init() {
	watchCmd.Flags().DurationVar(&watchPoll, "interval", 500*time.Millisecond, "")
}

const minWatchInterval = 100 * time.Millisecond

func runWatch(cmd *cobra.Command, args []string) error {
	if watchPoll < minWatchInterval {
		return fmt.Errorf("--interval must be at least %s", minWatchInterval)
	}

	store := capture.NewStore(0, config.StoreDir())
	dir := config.StoreDir()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "store directory does not exist: %s\n", dir)
	}

	seen := make(map[string]struct{})
	for _, c := range store.AllFromDisk() {
		seen[c.ID] = struct{}{}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	tick := time.NewTicker(watchPoll)
	defer tick.Stop()

	for {
		select {
		case <-sig:
			return nil
		case <-tick.C:
			var fresh []*capture.Capture
			for _, c := range store.AllFromDisk() {
				if _, ok := seen[c.ID]; ok {
					continue
				}
				seen[c.ID] = struct{}{}
				fresh = append(fresh, c)
			}
			sort.Slice(fresh, func(i, j int) bool {
				return fresh[i].Timestamp.Before(fresh[j].Timestamp)
			})
			for _, c := range fresh {
				printCaptureLine(c)
			}
		}
	}
}
