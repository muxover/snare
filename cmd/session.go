package cmd

import (
	"fmt"
	"time"

	"github.com/muxover/snare/capture"
	"github.com/muxover/snare/config"
	sess "github.com/muxover/snare/session"
	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Record and diff named capture sessions",
}

var sessionStartCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Mark the start of a named session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := sess.Load()
		if err != nil {
			return err
		}
		sessions[args[0]] = sess.Entry{Start: time.Now()}
		if err := sess.Save(sessions); err != nil {
			return err
		}
		fmt.Printf("Session %q started at %s\n", args[0], sessions[args[0]].Start.Format(time.RFC3339))
		return nil
	},
}

var sessionEndCmd = &cobra.Command{
	Use:   "end <name>",
	Short: "Mark the end of a named session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := sess.Load()
		if err != nil {
			return err
		}
		e, ok := sessions[args[0]]
		if !ok {
			return fmt.Errorf("no session %q — run 'snare session start %s' first", args[0], args[0])
		}
		e.End = time.Now()
		sessions[args[0]] = e
		if err := sess.Save(sessions); err != nil {
			return err
		}
		fmt.Printf("Session %q ended at %s\n", args[0], e.End.Format(time.RFC3339))
		return nil
	},
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all named sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := sess.Load()
		if err != nil {
			return err
		}
		if len(sessions) == 0 {
			fmt.Println("No sessions recorded.")
			return nil
		}
		for _, n := range sess.SortedNames(sessions) {
			e := sessions[n]
			if e.End.IsZero() {
				fmt.Printf("%-20s  started %s  (open)\n", n, e.Start.Format("15:04:05"))
			} else {
				fmt.Printf("%-20s  %s → %s\n", n, e.Start.Format("15:04:05"), e.End.Format("15:04:05"))
			}
		}
		return nil
	},
}

var sessionDiffCmd = &cobra.Command{
	Use:   "diff <session-a> <session-b>",
	Short: "Compare capture sequences from two sessions",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := sess.Load()
		if err != nil {
			return err
		}
		a, ok := sessions[args[0]]
		if !ok {
			return fmt.Errorf("unknown session %q", args[0])
		}
		b, ok := sessions[args[1]]
		if !ok {
			return fmt.Errorf("unknown session %q", args[1])
		}
		store := capture.NewStore(0, config.StoreDir())
		all := store.AllFromDisk()
		seqA := sess.Captures(all, a)
		seqB := sess.Captures(all, b)

		fmt.Printf("Session %q: %d requests\n", args[0], len(seqA))
		fmt.Printf("Session %q: %d requests\n\n", args[1], len(seqB))

		diffs := 0
		n := len(seqA)
		if len(seqB) > n {
			n = len(seqB)
		}
		for i := 0; i < n; i++ {
			var ca, cb *capture.Capture
			if i < len(seqA) {
				ca = seqA[i]
			}
			if i < len(seqB) {
				cb = seqB[i]
			}
			if ca == nil {
				fmt.Printf("[%d] only in %s: %s %s\n", i+1, args[1], cb.Request.Method, sess.RequestPath(cb))
				diffs++
				continue
			}
			if cb == nil {
				fmt.Printf("[%d] only in %s: %s %s\n", i+1, args[0], ca.Request.Method, sess.RequestPath(ca))
				diffs++
				continue
			}
			lineA := fmt.Sprintf("%s %s %d", ca.Request.Method, sess.RequestPath(ca), sess.ResponseStatus(ca))
			lineB := fmt.Sprintf("%s %s %d", cb.Request.Method, sess.RequestPath(cb), sess.ResponseStatus(cb))
			if lineA != lineB {
				fmt.Printf("[%d] %s → %s\n", i+1, lineA, lineB)
				diffs++
			}
		}
		if diffs == 0 {
			fmt.Println("Sessions match.")
		} else {
			fmt.Printf("\n%d difference(s) found.\n", diffs)
		}
		return nil
	},
}

var sessionDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a named session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessions, err := sess.Load()
		if err != nil {
			return err
		}
		if _, ok := sessions[args[0]]; !ok {
			return fmt.Errorf("unknown session %q", args[0])
		}
		delete(sessions, args[0])
		if err := sess.Save(sessions); err != nil {
			return err
		}
		fmt.Printf("Session %q deleted.\n", args[0])
		return nil
	},
}

func init() {
	sessionCmd.AddCommand(sessionStartCmd)
	sessionCmd.AddCommand(sessionEndCmd)
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionDiffCmd)
	sessionCmd.AddCommand(sessionDeleteCmd)
}
