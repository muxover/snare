package cmd

import (
	"fmt"
	"github.com/muxover/snare/config"
	"github.com/muxover/snare/proxy/cert"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

var caCmd = &cobra.Command{
	Use:   "ca",
	Short: "CA certificate commands",
	Long:  "Manage the proxy's CA certificate used for HTTPS MITM. generate: create CA if missing. install: print instructions to trust the CA on your system.",
}

var caGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate CA certificate if not present",
	RunE:  runCAGenerate,
}

var caInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Print instructions to install CA in system trust store",
	RunE:  runCAInstall,
}

func init() {
	caCmd.AddCommand(caGenerateCmd)
	caCmd.AddCommand(caInstallCmd)
}

func runCAGenerate(cmd *cobra.Command, args []string) error {
	dir := config.CADir()
	_, _, err := cert.LoadOrCreateCA(dir)
	if err != nil {
		return err
	}
	fmt.Println("CA is at", dir)
	return nil
}

func runCAInstall(cmd *cobra.Command, args []string) error {
	dir := config.CADir()
	certPath := dir + "/" + cert.CertFile
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		fmt.Println("Run 'snare ca generate' first.")
		return nil
	}
	fmt.Println("To trust Snare for HTTPS MITM, install the CA certificate:")
	fmt.Println("  Certificate file:", certPath)
	fmt.Println()
	switch runtime.GOOS {
	case "windows":
		fmt.Println("Windows: Double-click ca.pem → Install Certificate → Local Machine → Trusted Root CAs")
	case "darwin":
		fmt.Println("macOS: sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain " + certPath)
	case "linux":
		fmt.Println("Linux: copy to /usr/local/share/ca-certificates/ and run sudo update-ca-certificates")
	default:
		fmt.Println("Import", certPath, "into your system or browser trust store.")
	}
	return nil
}
