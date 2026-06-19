package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/config"
	"github.com/spf13/cobra"
)

var (
	watchPoll      time.Duration
	watchMethod    string
	watchStatus    int
	watchURL       string
	watchHost      string
	watchBody      string
	watchOperation string
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Print new captures as they are saved",
	Long:  "Poll the capture store and print one line per new capture. Supports the same filters as list. Use Ctrl+C to stop.",
	RunE:  runWatch,
}

func init() {
	watchCmd.Flags().DurationVar(&watchPoll, "interval", 500*time.Millisecond, "Poll interval")
	watchCmd.Flags().StringVar(&watchMethod, "method", "", "Filter by HTTP method")
	watchCmd.Flags().IntVar(&watchStatus, "status", 0, "Filter by response status code")
	watchCmd.Flags().StringVar(&watchURL, "url", "", "Filter by URL substring")
	watchCmd.Flags().StringVar(&watchHost, "host", "", "Filter by host")
	watchCmd.Flags().StringVar(&watchBody, "body", "", "Filter by substring in request or response body")
	watchCmd.Flags().StringVar(&watchOperation, "operation", "", "Filter by GraphQL operation name")
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
			filtered := filterCaptures(fresh, watchMethod, watchStatus, watchURL, watchHost, watchBody, watchOperation, time.Time{}, time.Time{})
			for _, c := range filtered {
				printCaptureLine(c)
			}
		}
	}
}
