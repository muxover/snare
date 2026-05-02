package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/muxover/snare/config"
	"github.com/muxover/snare/intercept"
	"github.com/spf13/cobra"
)

var interceptCmd = &cobra.Command{
	Use:   "intercept",
	Short: "Manage intercepted requests",
	Long:  "List, forward, edit, or drop requests held by the proxy when --intercept is active.",
}

var interceptListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pending intercepted requests",
	RunE:  runInterceptList,
}

var interceptForwardCmd = &cobra.Command{
	Use:   "forward [id]",
	Short: "Forward a held request to the origin as-is",
	Args:  cobra.ExactArgs(1),
	RunE:  runInterceptForward,
}

var interceptDropCmd = &cobra.Command{
	Use:   "drop [id]",
	Short: "Drop a held request (returns 502 to the client)",
	Args:  cobra.ExactArgs(1),
	RunE:  runInterceptDrop,
}

var interceptEditCmd = &cobra.Command{
	Use:   "edit [id]",
	Short: "Edit a held request in $EDITOR then forward it",
	Long:  "Opens the pending request as JSON in $EDITOR. Save and exit to forward the modified request.",
	Args:  cobra.ExactArgs(1),
	RunE:  runInterceptEdit,
}

func init() {
	interceptCmd.AddCommand(interceptListCmd)
	interceptCmd.AddCommand(interceptForwardCmd)
	interceptCmd.AddCommand(interceptDropCmd)
	interceptCmd.AddCommand(interceptEditCmd)
}

func interceptQueue() *intercept.Queue {
	return intercept.NewQueue(config.InterceptDir())
}

func runInterceptList(cmd *cobra.Command, args []string) error {
	pending, err := interceptQueue().Pending()
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		fmt.Println("No intercepted requests pending.")
		return nil
	}
	for _, pr := range pending {
		short := pr.ID
		if len(short) > 8 {
			short = short[:8]
		}
		fmt.Printf("%s  %s  %-7s  %s\n", short, pr.Timestamp.Format("15:04:05"), pr.Method, pr.URL)
	}
	return nil
}

func runInterceptForward(cmd *cobra.Command, args []string) error {
	q := interceptQueue()
	if err := q.Decide(args[0], intercept.DecisionForward, nil); err != nil {
		return err
	}
	fmt.Println("Forwarded.")
	return nil
}

func runInterceptDrop(cmd *cobra.Command, args []string) error {
	q := interceptQueue()
	if err := q.Decide(args[0], intercept.DecisionDrop, nil); err != nil {
		return err
	}
	fmt.Println("Dropped.")
	return nil
}

func runInterceptEdit(cmd *cobra.Command, args []string) error {
	q := interceptQueue()
	pr, err := q.GetByPrefix(args[0])
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "snare-intercept-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(pr); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		return fmt.Errorf("$EDITOR is not set")
	}
	c := exec.Command(editor, tmp.Name())
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return err
	}
	var edited intercept.PendingRequest
	if err := json.Unmarshal(data, &edited); err != nil {
		return fmt.Errorf("invalid JSON after edit: %w", err)
	}

	mod := &intercept.PendingRequest{
		ModMethod:  edited.ModMethod,
		ModHeaders: edited.ModHeaders,
		ModBody:    edited.ModBody,
	}
	if err := q.Decide(args[0], intercept.DecisionForward, mod); err != nil {
		return err
	}
	fmt.Println("Forwarded with edits.")
	return nil
}
