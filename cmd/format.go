package cmd

import (
	"fmt"
	"time"

	"github.com/muxover/snare/capture"
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

func printCaptureLine(c *capture.Capture) {
	status := "-  "
	if c.Response != nil {
		status = fmt.Sprintf("%-3d", c.Response.StatusCode)
	}
	if c.Error != "" {
		status = "err"
	}
	idShort := c.ID
	if len(idShort) > 8 {
		idShort = idShort[:8]
	}
	fmt.Printf("%s  %s  %-7s  %-7s  %s  %s\n",
		idShort,
		c.Timestamp.Format("15:04:05"),
		formatListLatency(c.Duration),
		c.Request.Method,
		status,
		c.Request.URL,
	)
}
