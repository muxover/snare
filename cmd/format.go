package cmd

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muxover/snare/capture"
)

var (
	colorGreen   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	colorYellow  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	colorRed     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	colorMagenta = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	colorCyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	colorBlue    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	colorBoldRed = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	colorFaint   = lipgloss.NewStyle().Faint(true)
)

func formatListLatency(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func colorStatus(status string, code int) string {
	switch {
	case code >= 500:
		return colorMagenta.Render(status)
	case code >= 400:
		return colorRed.Render(status)
	case code >= 300:
		return colorYellow.Render(status)
	case code >= 200:
		return colorGreen.Render(status)
	default:
		return status
	}
}

func colorProto(proto string) string {
	switch proto {
	case "h2":
		return colorBlue.Render(proto)
	case "ws":
		return colorCyan.Render(proto)
	default:
		return colorFaint.Render(proto)
	}
}

func printCaptureLine(c *capture.Capture) {
	idShort := c.ID
	if len(idShort) > 8 {
		idShort = idShort[:8]
	}

	proto := c.Protocol
	if proto == "" {
		proto = "h1"
	}

	status := "-  "
	statusCode := 0
	if c.Response != nil {
		statusCode = c.Response.StatusCode
		status = fmt.Sprintf("%-3d", statusCode)
	}

	line := fmt.Sprintf("%s  %s  %-7s  %-2s  %-7s  %s  %s",
		idShort,
		c.Timestamp.Format("15:04:05"),
		formatListLatency(c.Duration),
		colorProto(proto),
		c.Request.Method,
		func() string {
			if c.Error != "" {
				return colorBoldRed.Render("err")
			}
			return colorStatus(status, statusCode)
		}(),
		c.Request.URL,
	)
	fmt.Println(line)
}
