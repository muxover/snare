package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/muxover/snare/config"
	"github.com/muxover/snare/proxy/cert"
	"github.com/spf13/cobra"
)

var caCmd = &cobra.Command{
	Use:   "ca",
	Short: "CA certificate commands",
	Long:  "Manage the proxy's CA certificate used for HTTPS MITM. generate: create CA if missing. install: add the CA to your system trust store.",
}

var caGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate CA certificate if not present",
	RunE:  runCAGenerate,
}

var caInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install CA into system trust store",
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
	certPath := filepath.Join(dir, cert.CertFile)
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		fmt.Println("Run 'snare ca generate' first.")
		return nil
	}

	switch runtime.GOOS {
	case "windows":
		return installWindows(certPath)
	case "darwin":
		return installDarwin(certPath)
	case "linux":
		return installLinux(certPath)
	default:
		fmt.Println("Unsupported OS. Import manually:", certPath)
		return nil
	}
}

func installWindows(certPath string) error {
	fmt.Println("Installing CA into Windows trusted root store...")
	out, err := exec.Command("certutil", "-addstore", "-f", "Root", certPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("certutil failed: %w\n%s", err, out)
	}
	fmt.Println("Installed. You may need to restart your browser.")
	return nil
}

func installDarwin(certPath string) error {
	fmt.Println("Installing CA into macOS system keychain...")
	out, err := exec.Command("sudo", "security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", "/Library/Keychains/System.keychain", certPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("security command failed: %w\n%s", err, out)
	}
	fmt.Println("Installed.")
	return nil
}

func installLinux(certPath string) error {
	dest := "/usr/local/share/ca-certificates/snare-ca.crt"
	fmt.Printf("Copying CA to %s...\n", dest)

	src, err := os.Open(certPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("cannot write to %s (try sudo): %w", dest, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}

	fmt.Println("Running update-ca-certificates...")
	out, err := exec.Command("update-ca-certificates").CombinedOutput()
	if err != nil {
		return fmt.Errorf("update-ca-certificates failed: %w\n%s", err, out)
	}
	fmt.Println("Installed.")
	return nil
}
