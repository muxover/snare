package tui

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muxover/snare/capture"
)

const pollInterval = 2 * time.Second

var (
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleBar    = lipgloss.NewStyle().Faint(true)
	styleSel    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	styleErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleDim    = lipgloss.NewStyle().Faint(true)
	styleSec    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
)

type viewState int

const (
	viewList viewState = iota
	viewDetail
	viewFilter
)

type tickMsg time.Time

type Model struct {
	store       *capture.Store
	all         []*capture.Capture
	filtered    []*capture.Capture
	cursor      int
	vp          viewport.Model
	width       int
	height      int
	state       viewState
	filter      string
	filterDraft string
	notify      string
}

func New(store *capture.Store) Model {
	m := Model{store: store}
	m.reload()
	return m
}

func (m *Model) reload() {
	m.all = m.store.AllFromDisk()
	m.applyFilter()
}

func (m *Model) applyFilter() {
	if m.filter == "" {
		m.filtered = m.all
	} else {
		var out []*capture.Capture
		low := strings.ToLower(m.filter)
		for _, c := range m.all {
			if strings.Contains(strings.ToLower(c.Request.URL), low) ||
				strings.Contains(strings.ToLower(c.Request.Method), low) {
				out = append(out, c)
			}
		}
		m.filtered = out
	}
	if m.cursor >= len(m.filtered) && len(m.filtered) > 0 {
		m.cursor = len(m.filtered) - 1
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.vp = viewport.New(msg.Width, m.height-4)
		return m, nil

	case tickMsg:
		m.reload()
		if m.state == viewDetail && len(m.filtered) > 0 {
			m.vp.SetContent(renderDetail(m.filtered[m.cursor], m.width))
		}
		return m, tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.state == viewDetail {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case viewFilter:
		return m.filterKey(msg)
	case viewDetail:
		return m.detailKey(msg)
	default:
		return m.listKey(msg)
	}
}

func (m Model) listKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.notify = ""
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case "enter":
		if len(m.filtered) > 0 {
			m.vp = viewport.New(m.width, m.height-4)
			m.vp.SetContent(renderDetail(m.filtered[m.cursor], m.width))
			m.vp.GotoTop()
			m.state = viewDetail
		}
	case "r":
		if len(m.filtered) > 0 {
			c := m.filtered[m.cursor]
			go replayCapture(c)
			m.notify = "replaying " + c.ID[:8] + "…"
		}
	case "/":
		m.filterDraft = m.filter
		m.state = viewFilter
	case "esc":
		m.filter = ""
		m.applyFilter()
	}
	return m, nil
}

func (m Model) detailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.notify = ""
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "b":
		m.state = viewList
		return m, nil
	case "r":
		if len(m.filtered) > 0 {
			c := m.filtered[m.cursor]
			go replayCapture(c)
			m.notify = "replaying " + c.ID[:8] + "…"
		}
	default:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) filterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filter = m.filterDraft
		m.applyFilter()
		m.state = viewList
	case "esc":
		m.filterDraft = m.filter
		m.state = viewList
	case "backspace":
		if len(m.filterDraft) > 0 {
			runes := []rune(m.filterDraft)
			m.filterDraft = string(runes[:len(runes)-1])
		}
	default:
		if len(msg.Runes) > 0 {
			m.filterDraft += string(msg.Runes)
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	switch m.state {
	case viewDetail:
		return m.renderDetailView()
	default:
		return m.renderListView()
	}
}

func (m Model) renderListView() string {
	filterLabel := ""
	if m.state == viewFilter {
		filterLabel = "  filter: " + m.filterDraft + "▌"
	} else if m.filter != "" {
		filterLabel = "  filter: " + m.filter
	}

	title := styleHeader.Render("snare") + styleDim.Render(fmt.Sprintf(" · %d captures%s", len(m.filtered), filterLabel))
	sep := styleDim.Render(strings.Repeat("─", m.width))

	cols := styleBar.Render(fmt.Sprintf("  %-8s  %-8s  %-7s  %-3s  %-8s  %s",
		"ID", "TIME", "METHOD", "ST", "LATENCY", "URL"))

	visibleRows := m.height - 5
	if visibleRows < 1 {
		visibleRows = 1
	}

	start := 0
	if m.cursor >= visibleRows {
		start = m.cursor - visibleRows + 1
	}
	end := start + visibleRows
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	var rows []string
	for i := start; i < end; i++ {
		rows = append(rows, m.renderRow(i))
	}

	if len(m.filtered) == 0 {
		rows = []string{styleDim.Render("  no captures")}
	}

	hint := styleBar.Render("  ↑↓ navigate · enter inspect · r replay · / filter · esc clear · q quit")
	if m.notify != "" {
		hint = styleDim.Render("  " + m.notify)
	}

	lines := []string{title, sep, cols}
	lines = append(lines, rows...)
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	lines = append(lines, hint)
	return strings.Join(lines, "\n")
}

func (m Model) renderRow(i int) string {
	c := m.filtered[i]
	id := c.ID
	if len(id) > 8 {
		id = id[:8]
	}
	status := "-  "
	if c.Response != nil {
		status = fmt.Sprintf("%3d", c.Response.StatusCode)
	}
	if c.Error != "" {
		status = "err"
	}
	lat := fmtLatency(c.Duration)
	maxURL := m.width - 44
	if maxURL < 10 {
		maxURL = 10
	}
	u := c.Request.URL
	if len(u) > maxURL {
		u = u[:maxURL-1] + "…"
	}
	row := fmt.Sprintf("  %-8s  %-8s  %-7s  %-3s  %-8s  %s",
		id,
		c.Timestamp.Format("15:04:05"),
		c.Request.Method,
		status,
		lat,
		u,
	)
	if i == m.cursor {
		return styleSel.Render("▶" + row[1:])
	}
	if c.Error != "" {
		return styleErr.Render(row)
	}
	return row
}

func (m Model) renderDetailView() string {
	if len(m.filtered) == 0 {
		return ""
	}
	c := m.filtered[m.cursor]
	id := c.ID
	if len(id) > 8 {
		id = id[:8]
	}
	status := ""
	if c.Response != nil {
		status = fmt.Sprintf(" · %d", c.Response.StatusCode)
	}
	title := styleHeader.Render("snare") + styleDim.Render(fmt.Sprintf(" · %s · %s%s · %s", id, c.Request.Method, status, truncate(c.Request.URL, m.width-40)))
	sep := styleDim.Render(strings.Repeat("─", m.width))
	hint := styleBar.Render("  ↑↓ scroll · r replay · esc/b back · q quit")
	if m.notify != "" {
		hint = styleDim.Render("  " + m.notify)
	}
	return title + "\n" + sep + "\n" + m.vp.View() + "\n" + hint
}

func renderDetail(c *capture.Capture, width int) string {
	var b strings.Builder

	b.WriteString(styleSec.Render("── Request ") + strings.Repeat("─", max(0, width-12)) + "\n")
	b.WriteString(c.Request.Method + " " + c.Request.URL + "\n")
	for k, vals := range c.Request.Headers {
		for _, v := range vals {
			b.WriteString(styleDim.Render(k+": ") + v + "\n")
		}
	}
	if len(c.Request.Body) > 0 {
		b.WriteString("\n" + string(c.Request.Body) + "\n")
	}

	if c.Response != nil {
		b.WriteString("\n" + styleSec.Render("── Response ") + strings.Repeat("─", max(0, width-13)) + "\n")
		b.WriteString(fmt.Sprintf("HTTP %d\n", c.Response.StatusCode))
		for k, vals := range c.Response.Headers {
			for _, v := range vals {
				b.WriteString(styleDim.Render(k+": ") + v + "\n")
			}
		}
		if len(c.Response.Body) > 0 {
			b.WriteString("\n" + string(c.Response.Body) + "\n")
		}
	}

	if c.Error != "" {
		b.WriteString("\n" + styleErr.Render("Error: "+c.Error) + "\n")
	}

	return b.String()
}

func replayCapture(c *capture.Capture) {
	req, err := http.NewRequest(c.Request.Method, c.Request.URL, bytes.NewReader(c.Request.Body))
	if err != nil {
		return
	}
	for k, vals := range c.Request.Headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
}

func fmtLatency(d time.Duration) string {
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

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

