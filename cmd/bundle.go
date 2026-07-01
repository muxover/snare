package cmd

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/config"
	"github.com/muxover/snare/v2/mock"
	sess "github.com/muxover/snare/v2/session"

	"github.com/spf13/cobra"
)

var bundleCmd = &cobra.Command{
	Use:   "bundle",
	Short: "Pack and unpack capture bundles",
	Long:  "Pack captures, mocks, and sessions into a portable .snare bundle file, or unpack one to import everything back.",
}

var (
	bundlePackOut     string
	bundlePackSession string
	bundlePackIDs     string
)

var bundlePackCmd = &cobra.Command{
	Use:   "pack",
	Short: "Pack captures (+ mocks + sessions) into a .snare bundle",
	Long: `Pack captures, all mock rules, and all sessions into a gzip-compressed bundle file.

  snare bundle pack                          Pack all captures
  snare bundle pack --session baseline       Pack only captures from a named session
  snare bundle pack --ids abc123,def456      Pack specific captures by ID prefix`,
	RunE: runBundlePack,
}

var bundleUnpackCmd = &cobra.Command{
	Use:   "unpack <file.snare>",
	Short: "Import captures, mocks, and sessions from a bundle",
	Args:  cobra.ExactArgs(1),
	RunE:  runBundleUnpack,
}

func init() {
	bundlePackCmd.Flags().StringVarP(&bundlePackOut, "out", "o", "bundle.snare", "Output file")
	bundlePackCmd.Flags().StringVar(&bundlePackSession, "session", "", "Pack only captures from this session")
	bundlePackCmd.Flags().StringVar(&bundlePackIDs, "ids", "", "Comma-separated capture IDs (or prefixes) to pack")

	bundleCmd.AddCommand(bundlePackCmd)
	bundleCmd.AddCommand(bundleUnpackCmd)
}

type bundleRecord struct {
	Type string          `json:"type"`
	Name string          `json:"name,omitempty"`
	Data json.RawMessage `json:"data"`
}

func runBundlePack(cmd *cobra.Command, args []string) error {
	store := capture.NewStore(0, config.StoreDir())
	var captures []*capture.Capture

	switch {
	case bundlePackIDs != "":
		for _, id := range strings.Split(bundlePackIDs, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			c := store.GetByPrefix(id)
			if c == nil {
				return fmt.Errorf("capture not found: %s", id)
			}
			captures = append(captures, c)
		}
	case bundlePackSession != "":
		sessions, err := sess.Load()
		if err != nil {
			return err
		}
		e, ok := sessions[bundlePackSession]
		if !ok {
			return fmt.Errorf("unknown session %q", bundlePackSession)
		}
		all := store.AllFromDisk()
		captures = sess.Captures(all, e)
	default:
		captures = store.AllFromDisk()
	}

	mocks := mock.NewStore(config.MockFile()).Rules()
	sessions, err := sess.Load()
	if err != nil {
		return err
	}

	f, err := os.Create(bundlePackOut)
	if err != nil {
		return fmt.Errorf("creating bundle: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	enc := json.NewEncoder(gz)

	manifest := map[string]interface{}{
		"type":      "manifest",
		"version":   Version,
		"packed_at": time.Now().UTC().Format(time.RFC3339),
		"captures":  len(captures),
		"mocks":     len(mocks),
		"sessions":  len(sessions),
	}
	if err := enc.Encode(manifest); err != nil {
		return err
	}

	for _, c := range captures {
		data, err := json.Marshal(c)
		if err != nil {
			continue
		}
		if err := enc.Encode(bundleRecord{Type: "capture", Data: data}); err != nil {
			return err
		}
	}

	for _, r := range mocks {
		data, err := json.Marshal(r)
		if err != nil {
			continue
		}
		if err := enc.Encode(bundleRecord{Type: "mock", Data: data}); err != nil {
			return err
		}
	}

	for name, e := range sessions {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		if err := enc.Encode(bundleRecord{Type: "session", Name: name, Data: data}); err != nil {
			return err
		}
	}

	fmt.Printf("Packed → %s\n", bundlePackOut)
	fmt.Printf("  %d capture(s), %d mock(s), %d session(s)\n", len(captures), len(mocks), len(sessions))
	return nil
}

func runBundleUnpack(cmd *cobra.Command, args []string) error {
	f, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf("opening bundle: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("reading bundle (not a valid .snare file?): %w", err)
	}
	defer gz.Close()

	storeDir := config.StoreDir()
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		return err
	}

	mockStore := mock.NewStore(config.MockFile())
	sessions, err := sess.Load()
	if err != nil {
		return err
	}

	var capImported, capSkipped, mockImported, sessImported int

	dec := json.NewDecoder(gz)
	first := true
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return fmt.Errorf("reading bundle: %w", err)
		}
		if first {
			first = false
			continue // skip manifest line
		}

		var rec bundleRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}

		switch rec.Type {
		case "capture":
			var c capture.Capture
			if err := json.Unmarshal(rec.Data, &c); err != nil {
				continue
			}
			path := filepath.Join(storeDir, c.ID+".json")
			if _, err := os.Stat(path); err == nil {
				capSkipped++
				continue
			}
			if err := os.WriteFile(path, rec.Data, 0600); err != nil {
				return fmt.Errorf("writing capture %s: %w", c.ID, err)
			}
			capImported++

		case "mock":
			var r mock.Rule
			if err := json.Unmarshal(rec.Data, &r); err != nil {
				continue
			}
			dup := false
			for _, existing := range mockStore.Rules() {
				if existing.ID == r.ID {
					dup = true
					break
				}
			}
			if !dup {
				if err := mockStore.Add(&r); err != nil {
					return fmt.Errorf("adding mock: %w", err)
				}
				mockImported++
			}

		case "session":
			if rec.Name == "" {
				continue
			}
			if _, exists := sessions[rec.Name]; exists {
				continue
			}
			var e sess.Entry
			if err := json.Unmarshal(rec.Data, &e); err != nil {
				continue
			}
			sessions[rec.Name] = e
			sessImported++
		}
	}

	if sessImported > 0 {
		if err := sess.Save(sessions); err != nil {
			return err
		}
	}

	fmt.Printf("Unpacked from %s\n", args[0])
	fmt.Printf("  captures: %d imported, %d skipped (ID collision)\n", capImported, capSkipped)
	fmt.Printf("  mocks:    %d imported\n", mockImported)
	fmt.Printf("  sessions: %d imported\n", sessImported)
	return nil
}
