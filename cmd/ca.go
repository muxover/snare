package cmd

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/muxover/snare/config"
	"github.com/muxover/snare/proxy/cert"
	"github.com/spf13/cobra"
)

var caInstallDevice string

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
	caInstallCmd.Flags().StringVar(&caInstallDevice, "device", "", "Install on a connected device: ios or android")
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

	switch caInstallDevice {
	case "android":
		return installAndroid(certPath)
	case "ios":
		return installIOS(certPath)
	case "":
		return installSystem(certPath)
	default:
		return fmt.Errorf("unknown device %q — use ios or android", caInstallDevice)
	}
}

func installSystem(certPath string) error {
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

func installAndroid(certPath string) error {
	fmt.Println("Pushing CA to Android device via ADB...")
	out, err := exec.Command("adb", "push", certPath, "/sdcard/Download/snare-ca.pem").CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb push failed: %w\n%s", err, out)
	}
	exec.Command("adb", "shell", "am", "start", "-a", "android.settings.SECURITY_SETTINGS").Run()
	fmt.Println("Certificate pushed to /sdcard/Download/snare-ca.pem")
	fmt.Println("On the device: Settings → Security → Install from storage → select snare-ca.pem")
	fmt.Println("(Android 11+: Settings → Security → Advanced → Encryption & credentials → Install a certificate → CA certificate)")
	return nil
}

func installIOS(certPath string) error {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	localIP := localServerIP()
	serveURL := fmt.Sprintf("http://%s:%d/snare-ca.pem", localIP, port)

	mux := http.NewServeMux()
	mux.HandleFunc("/snare-ca.pem", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		http.ServeFile(w, r, certPath)
	})
	srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}

	fmt.Printf("On your iOS device, open Safari and go to:\n  %s\n", serveURL)
	fmt.Println("Tap 'Allow' → go to Settings → Profile Downloaded → Install → trust the certificate.")
	fmt.Println("Serving cert... press Ctrl+C when done.")

	go func() {
		time.Sleep(2 * time.Minute)
		srv.Close()
	}()
	_ = srv.ListenAndServe()
	return nil
}

func localServerIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "127.0.0.1"
}
