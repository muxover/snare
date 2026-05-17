package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/intercept"
	"github.com/muxover/snare/v2/mock"
	sess "github.com/muxover/snare/v2/session"
)

const pollInterval = 2 * time.Second

var (
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleBar    = lipgloss.NewStyle().Faint(true)
	styleSel    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	styleErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleDim    = lipgloss.NewStyle().Faint(true)
	styleSec    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	styleWS     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleMatch  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleMiss   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleActive = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

type tabState int

const (
	tabCaptures tabState = iota
	tabMocks
	tabIntercept
	tabSessions
)

type viewState int

const (
	viewList viewState = iota
	viewDetail
	viewFilter
	viewMockAdd
	viewICEdit
	viewSessStart
	viewSessDiff
	viewReplayEdit
)

type tickMsg time.Time

type Model struct {
	store     *capture.Store
	mocks     *mock.Store
	intercept *intercept.Queue

	tab    tabState
	state  viewState
	width  int
	height int
	notify string

	// captures tab
	all         []*capture.Capture
	filtered    []*capture.Capture
	cursor      int
	vp          viewport.Model
	filter      string
	filterDraft string
	diffA       string

	// mocks tab
	mockRules  []*mock.Rule
	mockCursor int
	mockInputs [5]textinput.Model
	mockFocus  int

	// intercept tab
	pending  []*intercept.PendingRequest
	icCursor int
	icInputs [3]textinput.Model
	icFocus  int
	icEditID string

	// replay edit
	replayInputs [4]textinput.Model
	replayFocus  int

	// sessions tab
	sessions   map[string]sess.Entry
	sessNames  []string
	sessCursor int
	sessInput  textinput.Model
	sessA      string

	proxyURL     string
	clearConfirm bool
}

func New(store *capture.Store, mocks *mock.Store, iq *intercept.Queue, proxyURL string) Model {
	si := textinput.New()
	si.Placeholder = "session-name"
	si.Width = 30

	m := Model{
		store:     store,
		mocks:     mocks,
		intercept: iq,
		sessions:  make(map[string]sess.Entry),
		sessInput: si,
		proxyURL:  proxyURL,
	}
	m.reload()
	m.reloadMocks()
	m.reloadIntercept()
	m.reloadSessions()
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
		low := strings.ToLower(m.filter)
		var out []*capture.Capture
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

func (m *Model) reloadMocks() {
	if m.mocks != nil {
		m.mockRules = m.mocks.Rules()
	}
}

func (m *Model) reloadIntercept() {
	if m.intercept != nil {
		m.pending, _ = m.intercept.Pending()
	}
}

func (m *Model) reloadSessions() {
	sessions, err := sess.Load()
	if err != nil {
		return
	}
	m.sessions = sessions
	m.sessNames = sess.SortedNames(sessions)
	if m.sessCursor >= len(m.sessNames) && len(m.sessNames) > 0 {
		m.sessCursor = len(m.sessNames) - 1
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
		m.reloadMocks()
		m.reloadIntercept()
		m.reloadSessions()
		if m.state == viewDetail || m.state == viewSessDiff {
			if len(m.filtered) > 0 {
				m.vp.SetContent(renderDetail(m.filtered[m.cursor], m.width))
			}
		}
		return m, tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	switch m.state {
	case viewMockAdd:
		var cmd tea.Cmd
		m.mockInputs[m.mockFocus], cmd = m.mockInputs[m.mockFocus].Update(msg)
		return m, cmd
	case viewICEdit:
		var cmd tea.Cmd
		m.icInputs[m.icFocus], cmd = m.icInputs[m.icFocus].Update(msg)
		return m, cmd
	case viewReplayEdit:
		var cmd tea.Cmd
		m.replayInputs[m.replayFocus], cmd = m.replayInputs[m.replayFocus].Update(msg)
		return m, cmd
	case viewSessStart:
		var cmd tea.Cmd
		m.sessInput, cmd = m.sessInput.Update(msg)
		return m, cmd
	case viewDetail, viewSessDiff:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case viewMockAdd:
		return m.mockAddKey(msg)
	case viewICEdit:
		return m.icEditKey(msg)
	case viewReplayEdit:
		return m.replayEditKey(msg)
	case viewSessStart:
		return m.sessStartKey(msg)
	}

	switch msg.String() {
	case "1":
		m.tab = tabCaptures
		m.state = viewList
		return m, nil
	case "2":
		m.tab = tabMocks
		m.state = viewList
		m.reloadMocks()
		return m, nil
	case "3":
		m.tab = tabIntercept
		m.state = viewList
		m.reloadIntercept()
		return m, nil
	case "4":
		m.tab = tabSessions
		m.state = viewList
		m.reloadSessions()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}

	switch m.state {
	case viewFilter:
		return m.filterKey(msg)
	case viewDetail:
		return m.detailKey(msg)
	case viewSessDiff:
		return m.sessDiffKey(msg)
	default:
		switch m.tab {
		case tabMocks:
			return m.mockListKey(msg)
		case tabIntercept:
			return m.icListKey(msg)
		case tabSessions:
			return m.sessListKey(msg)
		default:
			return m.listKey(msg)
		}
	}
}

func (m Model) listKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.notify = ""
	if m.clearConfirm {
		switch msg.String() {
		case "y", "C":
			m.store.Clear(true)
			m.diffA = ""
			m.reload()
			m.notify = "all captures deleted"
		default:
			m.notify = "clear cancelled"
		}
		m.clearConfirm = false
		return m, nil
	}
	switch msg.String() {
	case "q":
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
			go replayCapture(c, m.proxyURL)
			m.notify = "replaying " + c.ID[:8] + "…"
		}
	case "d":
		if len(m.filtered) > 0 {
			c := m.filtered[m.cursor]
			_ = m.store.DeleteByID(c.ID)
			if m.diffA == c.ID {
				m.diffA = ""
			}
			m.reload()
			m.notify = "deleted"
		}
	case "C":
		if len(m.all) > 0 {
			m.clearConfirm = true
			m.notify = "press y or C to clear all captures, any other key to cancel"
		}
	case " ":
		if len(m.filtered) > 0 {
			id := m.filtered[m.cursor].ID
			if m.diffA == id {
				m.diffA = ""
				m.notify = "diff mark cleared"
			} else {
				m.diffA = id
				m.notify = "marked for diff — navigate to another and press D"
			}
		}
	case "D":
		if m.diffA != "" && len(m.filtered) > 0 {
			cur := m.filtered[m.cursor]
			if m.diffA != cur.ID {
				var capA *capture.Capture
				for _, c := range m.all {
					if c.ID == m.diffA {
						capA = c
						break
					}
				}
				if capA != nil {
					m.vp = viewport.New(m.width, m.height-4)
					m.vp.SetContent(renderCaptureDiff(capA, cur, m.width))
					m.vp.GotoTop()
					m.state = viewDetail
				}
			}
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
			go replayCapture(c, m.proxyURL)
			m.notify = "replaying " + c.ID[:8] + "…"
		}
	case "e":
		if len(m.filtered) > 0 {
			c := m.filtered[m.cursor]
			m.replayInputs = initReplayInputs(c)
			m.replayFocus = 0
			m.state = viewReplayEdit
		}
	case "m":
		if len(m.filtered) > 0 {
			c := m.filtered[m.cursor]
			m.mockInputs = initMockInputsFromCapture(c)
			m.mockFocus = 0
			m.tab = tabMocks
			m.state = viewMockAdd
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

func (m Model) mockListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.notify = ""
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.mockCursor > 0 {
			m.mockCursor--
		}
	case "down", "j":
		if m.mockCursor < len(m.mockRules)-1 {
			m.mockCursor++
		}
	case "a":
		inputs := initMockInputs()
		m.mockInputs = inputs
		m.mockFocus = 0
		m.state = viewMockAdd
	case "d":
		if len(m.mockRules) > 0 {
			rule := m.mockRules[m.mockCursor]
			_, _ = m.mocks.Remove(rule.ID)
			m.reloadMocks()
			if m.mockCursor >= len(m.mockRules) && m.mockCursor > 0 {
				m.mockCursor--
			}
			m.notify = "mock removed"
		}
	case "C":
		if len(m.mockRules) > 0 {
			for _, r := range m.mockRules {
				_, _ = m.mocks.Remove(r.ID)
			}
			m.mockCursor = 0
			m.reloadMocks()
			m.notify = "all mock rules cleared"
		}
	}
	return m, nil
}

func (m Model) mockAddKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = viewList
		return m, nil
	case "tab", "down":
		m.mockInputs[m.mockFocus].Blur()
		m.mockFocus = (m.mockFocus + 1) % len(m.mockInputs)
		m.mockInputs[m.mockFocus].Focus()
		return m, nil
	case "shift+tab", "up":
		m.mockInputs[m.mockFocus].Blur()
		m.mockFocus = (m.mockFocus + len(m.mockInputs) - 1) % len(m.mockInputs)
		m.mockInputs[m.mockFocus].Focus()
		return m, nil
	case "enter":
		if m.mockFocus < len(m.mockInputs)-1 {
			m.mockInputs[m.mockFocus].Blur()
			m.mockFocus++
			m.mockInputs[m.mockFocus].Focus()
			return m, nil
		}
		return m.submitMock()
	default:
		var cmd tea.Cmd
		m.mockInputs[m.mockFocus], cmd = m.mockInputs[m.mockFocus].Update(msg)
		return m, cmd
	}
}

func (m Model) submitMock() (tea.Model, tea.Cmd) {
	method := strings.ToUpper(m.mockInputs[0].Value())
	urlMatch := m.mockInputs[1].Value()
	if urlMatch == "" {
		m.notify = "URL match is required"
		return m, nil
	}
	status, _ := strconv.Atoi(m.mockInputs[2].Value())
	if status == 0 {
		status = 200
	}
	ct := m.mockInputs[3].Value()
	if ct == "" {
		ct = "application/json"
	}
	rule := &mock.Rule{
		ID:          uuid.New().String(),
		Method:      method,
		URLPattern:  urlMatch,
		Status:      status,
		ContentType: ct,
		Body:        m.mockInputs[4].Value(),
	}
	if err := m.mocks.Add(rule); err != nil {
		m.notify = "error: " + err.Error()
	} else {
		m.notify = "mock added"
	}
	m.reloadMocks()
	m.state = viewList
	return m, nil
}

func (m Model) icListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.notify = ""
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.icCursor > 0 {
			m.icCursor--
		}
	case "down", "j":
		if m.icCursor < len(m.pending)-1 {
			m.icCursor++
		}
	case "r":
		m.reloadIntercept()
	case "f":
		if len(m.pending) > 0 {
			pr := m.pending[m.icCursor]
			if err := m.intercept.Decide(pr.ID, intercept.DecisionForward, nil); err != nil {
				m.notify = "error: " + err.Error()
			} else {
				m.notify = "forwarded"
			}
			m.reloadIntercept()
		}
	case "x":
		if len(m.pending) > 0 {
			pr := m.pending[m.icCursor]
			if err := m.intercept.Decide(pr.ID, intercept.DecisionDrop, nil); err != nil {
				m.notify = "error: " + err.Error()
			} else {
				m.notify = "dropped"
			}
			m.reloadIntercept()
		}
	case "e":
		if len(m.pending) > 0 {
			pr := m.pending[m.icCursor]
			m.icEditID = pr.ID
			m.icInputs = initICInputs(pr)
			m.icFocus = 0
			m.state = viewICEdit
		}
	}
	return m, nil
}

func (m Model) icEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = viewList
		return m, nil
	case "tab", "down":
		m.icInputs[m.icFocus].Blur()
		m.icFocus = (m.icFocus + 1) % len(m.icInputs)
		m.icInputs[m.icFocus].Focus()
		return m, nil
	case "shift+tab", "up":
		m.icInputs[m.icFocus].Blur()
		m.icFocus = (m.icFocus + len(m.icInputs) - 1) % len(m.icInputs)
		m.icInputs[m.icFocus].Focus()
		return m, nil
	case "enter":
		if m.icFocus < len(m.icInputs)-1 {
			m.icInputs[m.icFocus].Blur()
			m.icFocus++
			m.icInputs[m.icFocus].Focus()
			return m, nil
		}
		return m.submitICEdit()
	default:
		var cmd tea.Cmd
		m.icInputs[m.icFocus], cmd = m.icInputs[m.icFocus].Update(msg)
		return m, cmd
	}
}

func (m Model) submitICEdit() (tea.Model, tea.Cmd) {
	mod := &intercept.PendingRequest{
		ModMethod: m.icInputs[0].Value(),
		ModBody:   m.icInputs[2].Value(),
	}
	headersStr := m.icInputs[1].Value()
	if headersStr != "" {
		var flat map[string]string
		if json.Unmarshal([]byte(headersStr), &flat) == nil {
			h := make(http.Header)
			for k, v := range flat {
				h.Set(k, v)
			}
			mod.ModHeaders = h
		}
	}
	if err := m.intercept.Decide(m.icEditID, intercept.DecisionForward, mod); err != nil {
		m.notify = "error: " + err.Error()
	} else {
		m.notify = "forwarded with edits"
	}
	m.reloadIntercept()
	m.state = viewList
	return m, nil
}

func (m Model) replayEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = viewDetail
		return m, nil
	case "tab", "down":
		m.replayInputs[m.replayFocus].Blur()
		m.replayFocus = (m.replayFocus + 1) % len(m.replayInputs)
		m.replayInputs[m.replayFocus].Focus()
		return m, nil
	case "shift+tab", "up":
		m.replayInputs[m.replayFocus].Blur()
		m.replayFocus = (m.replayFocus + len(m.replayInputs) - 1) % len(m.replayInputs)
		m.replayInputs[m.replayFocus].Focus()
		return m, nil
	case "enter":
		if m.replayFocus < len(m.replayInputs)-1 {
			m.replayInputs[m.replayFocus].Blur()
			m.replayFocus++
			m.replayInputs[m.replayFocus].Focus()
			return m, nil
		}
		return m.submitReplayEdit()
	default:
		var cmd tea.Cmd
		m.replayInputs[m.replayFocus], cmd = m.replayInputs[m.replayFocus].Update(msg)
		return m, cmd
	}
}

func (m Model) submitReplayEdit() (tea.Model, tea.Cmd) {
	method := m.replayInputs[0].Value()
	if method == "" {
		method = "GET"
	}
	rawURL := m.replayInputs[1].Value()
	headersStr := m.replayInputs[2].Value()
	body := m.replayInputs[3].Value()
	var hdrs http.Header
	if headersStr != "" {
		var flat map[string]string
		if json.Unmarshal([]byte(headersStr), &flat) == nil {
			hdrs = make(http.Header)
			for k, v := range flat {
				hdrs.Set(k, v)
			}
		}
	}
	proxyURL := m.proxyURL
	go func() {
		req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
		if err != nil {
			return
		}
		for k, vs := range hdrs {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		var transport http.RoundTripper
		if proxyURL != "" {
			if pu, err := url.Parse(proxyURL); err == nil {
				transport = &http.Transport{Proxy: http.ProxyURL(pu)}
			}
		}
		client := &http.Client{Transport: transport}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
	}()
	m.notify = "replaying with edits…"
	m.state = viewDetail
	return m, nil
}

func (m Model) sessListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.notify = ""
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.sessCursor > 0 {
			m.sessCursor--
		}
	case "down", "j":
		if m.sessCursor < len(m.sessNames)-1 {
			m.sessCursor++
		}
	case "s":
		m.sessInput.SetValue("")
		m.sessInput.Focus()
		m.state = viewSessStart
	case "e":
		if len(m.sessNames) > 0 {
			name := m.sessNames[m.sessCursor]
			e, ok := m.sessions[name]
			if ok && e.End.IsZero() {
				e.End = time.Now()
				m.sessions[name] = e
				_ = sess.Save(m.sessions)
				m.reloadSessions()
				m.notify = "session ended"
			} else {
				m.notify = "session already ended"
			}
		}
	case "x":
		if len(m.sessNames) > 0 {
			name := m.sessNames[m.sessCursor]
			delete(m.sessions, name)
			_ = sess.Save(m.sessions)
			if m.sessA == name {
				m.sessA = ""
			}
			m.reloadSessions()
			m.notify = "session deleted"
		}
	case " ":
		if len(m.sessNames) > 0 {
			name := m.sessNames[m.sessCursor]
			if m.sessA == name {
				m.sessA = ""
				m.notify = "diff mark cleared"
			} else {
				m.sessA = name
				m.notify = "marked — navigate to another session and press D"
			}
		}
	case "D":
		if m.sessA != "" && len(m.sessNames) > 0 {
			nameB := m.sessNames[m.sessCursor]
			if m.sessA != nameB {
				eA, okA := m.sessions[m.sessA]
				eB, okB := m.sessions[nameB]
				if okA && okB {
					all := m.store.AllFromDisk()
					seqA := sess.Captures(all, eA)
					seqB := sess.Captures(all, eB)
					m.vp = viewport.New(m.width, m.height-4)
					m.vp.SetContent(renderSessionDiff(seqA, seqB, m.sessA, nameB, m.width))
					m.vp.GotoTop()
					m.state = viewSessDiff
				}
			}
		}
	}
	return m, nil
}

func (m Model) sessStartKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = viewList
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.sessInput.Value())
		if name == "" {
			m.notify = "name required"
			m.state = viewList
			return m, nil
		}
		m.sessions[name] = sess.Entry{Start: time.Now()}
		_ = sess.Save(m.sessions)
		m.reloadSessions()
		m.notify = "session started: " + name
		m.state = viewList
		return m, nil
	default:
		var cmd tea.Cmd
		m.sessInput, cmd = m.sessInput.Update(msg)
		return m, cmd
	}
}

func (m Model) sessDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "b", "q":
		m.state = viewList
		return m, nil
	default:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	return m.renderHeader() + "\n" +
		styleDim.Render(strings.Repeat("─", m.width)) + "\n" +
		m.renderBody()
}

func (m Model) renderHeader() string {
	tabs := []struct {
		key   string
		label string
		t     tabState
	}{
		{"1", "Captures", tabCaptures},
		{"2", "Mocks", tabMocks},
		{"3", "Intercept", tabIntercept},
		{"4", "Sessions", tabSessions},
	}
	var parts []string
	for _, tab := range tabs {
		label := tab.key + " " + tab.label
		if tab.t == m.tab {
			parts = append(parts, styleSel.Render("["+label+"]"))
		} else {
			parts = append(parts, styleDim.Render("["+label+"]"))
		}
	}
	return styleHeader.Render("snare") + " " + strings.Join(parts, " ")
}

func (m Model) renderBody() string {
	switch m.tab {
	case tabMocks:
		return m.renderMocksBody()
	case tabIntercept:
		return m.renderInterceptBody()
	case tabSessions:
		return m.renderSessionsBody()
	default:
		return m.renderCapturesBody()
	}
}

func (m Model) renderCapturesBody() string {
	switch m.state {
	case viewDetail:
		return m.renderDetailView()
	case viewReplayEdit:
		return m.renderReplayEdit()
	default:
		return m.renderListView()
	}
}

func (m Model) renderReplayEdit() string {
	labels := []string{"Method", "URL", "Headers (JSON)", "Body"}
	var lines []string
	lines = append(lines, styleSec.Render("Edit & Replay"))
	lines = append(lines, "")
	for i, inp := range m.replayInputs {
		label := styleBar.Render(fmt.Sprintf("  %-16s", labels[i]+":"))
		lines = append(lines, label+" "+inp.View())
	}
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, styleBar.Render("  tab next · shift+tab prev · enter next/submit · esc back"))
	return strings.Join(lines, "\n")
}

func (m Model) renderListView() string {
	filterLabel := ""
	if m.state == viewFilter {
		filterLabel = "  filter: " + m.filterDraft + "▌"
	} else if m.filter != "" {
		filterLabel = "  filter: " + m.filter
	}

	title := styleDim.Render(fmt.Sprintf("%d captures%s", len(m.filtered), filterLabel))
	if m.diffA != "" {
		title += styleActive.Render("  [diff: " + m.diffA[:8] + " marked]")
	}

	cols := styleBar.Render(fmt.Sprintf("  %-8s  %-8s  %-7s  %-3s  %-8s  %s",
		"ID", "TIME", "METHOD", "ST", "LATENCY", "URL"))

	visibleRows := m.height - 6
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

	hint := "  ↑↓ navigate · enter inspect · r replay · d delete · C clear all · space mark diff · D diff · / filter · esc clear filter · 1-4 tabs · q quit"
	if m.notify != "" {
		hint = "  " + m.notify
	}

	lines := []string{title, cols}
	lines = append(lines, rows...)
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, styleBar.Render(hint))
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
	maxURL := m.width - 44
	if maxURL < 10 {
		maxURL = 10
	}
	u := c.Request.URL
	if len(u) > maxURL {
		u = u[:maxURL-1] + "…"
	}
	row := fmt.Sprintf("  %-8s  %-8s  %-7s  %-3s  %-8s  %s",
		id, c.Timestamp.Format("15:04:05"), c.Request.Method, status, fmtLatency(c.Duration), u)
	if i == m.cursor {
		prefix := "▶"
		if m.diffA == c.ID {
			prefix = "◆"
		}
		return styleSel.Render(prefix + row[1:])
	}
	if m.diffA == c.ID {
		return styleActive.Render("◆" + row[1:])
	}
	if c.Error != "" {
		return styleErr.Render(row)
	}
	return row
}

func (m Model) renderDetailView() string {
	if len(m.filtered) == 0 && m.state == viewDetail {
		return ""
	}
	hint := styleBar.Render("  ↑↓ scroll · r replay · e edit&replay · m mock · esc/b back · q quit")
	if m.notify != "" {
		hint = styleDim.Render("  " + m.notify)
	}
	return m.vp.View() + "\n" + hint
}

func (m Model) renderMocksBody() string {
	if m.state == viewMockAdd {
		return m.renderMockAdd()
	}
	return m.renderMockList()
}

func (m Model) renderMockList() string {
	title := styleDim.Render(fmt.Sprintf("%d mock rules", len(m.mockRules)))
	var rows []string
	for i, r := range m.mockRules {
		method := r.Method
		if method == "" {
			method = "*"
		}
		line := fmt.Sprintf("  %-6s  %-30s  →  %d", method, truncate(r.URLPattern, 30), r.Status)
		if i == m.mockCursor {
			rows = append(rows, styleSel.Render("▶"+line[1:]))
		} else {
			rows = append(rows, line)
		}
	}
	if len(m.mockRules) == 0 {
		rows = []string{styleDim.Render("  no mock rules")}
	}
	hint := styleBar.Render("  ↑↓ navigate · a add · d delete · C clear all · 1-4 tabs · q quit")
	if m.notify != "" {
		hint = "  " + m.notify
	}
	lines := []string{title}
	lines = append(lines, rows...)
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, hint)
	return strings.Join(lines, "\n")
}

func (m Model) renderMockAdd() string {
	labels := []string{"Method", "URL match", "Status", "Content-Type", "Body"}
	var lines []string
	lines = append(lines, styleSec.Render("Add mock rule"))
	lines = append(lines, "")
	for i, inp := range m.mockInputs {
		label := styleBar.Render(fmt.Sprintf("  %-14s", labels[i]+":"))
		lines = append(lines, label+" "+inp.View())
	}
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, styleBar.Render("  tab next · shift+tab prev · enter next/submit · esc cancel"))
	return strings.Join(lines, "\n")
}

func (m Model) renderInterceptBody() string {
	if m.state == viewICEdit {
		return m.renderICEdit()
	}
	return m.renderICList()
}

func (m Model) renderICList() string {
	label := "no pending requests"
	if m.intercept == nil {
		label = "intercept not enabled (start proxy with --intercept)"
	}
	title := styleDim.Render(fmt.Sprintf("%d pending", len(m.pending)))
	var rows []string
	for i, pr := range m.pending {
		line := fmt.Sprintf("  %-7s  %s", pr.Method, truncate(pr.URL, m.width-14))
		if i == m.icCursor {
			rows = append(rows, styleSel.Render("▶"+line[1:]))
		} else {
			rows = append(rows, line)
		}
	}
	if len(m.pending) == 0 {
		rows = []string{styleDim.Render("  " + label)}
	}
	hint := styleBar.Render("  ↑↓ navigate · f forward · x drop · e edit · r reload · 1-4 tabs · q quit")
	if m.notify != "" {
		hint = "  " + m.notify
	}
	lines := []string{title}
	lines = append(lines, rows...)
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, hint)
	return strings.Join(lines, "\n")
}

func (m Model) renderICEdit() string {
	labels := []string{"Method", "Headers (JSON)", "Body"}
	var lines []string
	lines = append(lines, styleSec.Render("Edit & Forward"))
	lines = append(lines, "")
	for i, inp := range m.icInputs {
		label := styleBar.Render(fmt.Sprintf("  %-16s", labels[i]+":"))
		lines = append(lines, label+" "+inp.View())
	}
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, styleBar.Render("  tab next · shift+tab prev · enter next/submit · esc cancel"))
	return strings.Join(lines, "\n")
}

func (m Model) renderSessionsBody() string {
	switch m.state {
	case viewSessStart:
		return m.renderSessStart()
	case viewSessDiff:
		hint := styleBar.Render("  ↑↓ scroll · esc/b back")
		return m.vp.View() + "\n" + hint
	default:
		return m.renderSessList()
	}
}

func (m Model) renderSessList() string {
	title := styleDim.Render(fmt.Sprintf("%d sessions", len(m.sessNames)))
	if m.sessA != "" {
		title += styleActive.Render("  [diff: " + m.sessA + " marked]")
	}
	var rows []string
	for i, name := range m.sessNames {
		e := m.sessions[name]
		var status string
		if e.End.IsZero() {
			status = styleActive.Render("active")
		} else {
			status = styleDim.Render(e.Start.Format("15:04:05") + " → " + e.End.Format("15:04:05"))
		}
		prefix := "  "
		if name == m.sessA {
			prefix = styleActive.Render("◆ ")
		}
		line := fmt.Sprintf("%s%-20s  %s", prefix, name, status)
		if i == m.sessCursor {
			rows = append(rows, styleSel.Render("▶ "+name))
		} else {
			rows = append(rows, line)
		}
	}
	if len(m.sessNames) == 0 {
		rows = []string{styleDim.Render("  no sessions — press s to start one")}
	}
	hint := styleBar.Render("  ↑↓ navigate · s start · e end · x delete · space mark diff · D diff · 1-4 tabs · q quit")
	if m.notify != "" {
		hint = "  " + m.notify
	}
	lines := []string{title}
	lines = append(lines, rows...)
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, hint)
	return strings.Join(lines, "\n")
}

func (m Model) renderSessStart() string {
	var lines []string
	lines = append(lines, styleSec.Render("Start session"))
	lines = append(lines, "")
	lines = append(lines, styleBar.Render("  Name: ")+m.sessInput.View())
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}
	lines = append(lines, styleBar.Render("  enter to start · esc to cancel"))
	return strings.Join(lines, "\n")
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

	if c.GRPC != nil && len(c.GRPC.Frames) > 0 {
		b.WriteString("\n" + styleSec.Render("── gRPC ") + strings.Repeat("─", max(0, width-9)) + "\n")
		b.WriteString(styleDim.Render("method: "+c.GRPC.ServiceMethod) + "\n")
		for _, f := range c.GRPC.Frames {
			dir := "→"
			if f.Direction == "response" {
				dir = "←"
			}
			data := string(f.Data)
			if len(data) > 256 {
				data = data[:256] + "…"
			}
			b.WriteString(styleWS.Render(dir) + " " + styleDim.Render(data) + "\n")
		}
	}

	if c.WebSocket != nil && len(c.WebSocket.Frames) > 0 {
		b.WriteString("\n" + styleSec.Render("── WebSocket Frames ") + strings.Repeat("─", max(0, width-21)) + "\n")
		for _, f := range c.WebSocket.Frames {
			dir := "→"
			if f.Direction == "server" {
				dir = "←"
			}
			opcode := wsOpcodeName(f.Opcode)
			header := styleWS.Render(fmt.Sprintf("%s %s %s", dir, f.Timestamp.Format("15:04:05.000"), opcode))
			b.WriteString(header + "\n")
			if len(f.Payload) > 0 {
				payload := string(f.Payload)
				if len(payload) > 512 {
					payload = payload[:512] + "…"
				}
				b.WriteString(styleDim.Render(payload) + "\n")
			}
		}
	}

	if c.Error != "" {
		b.WriteString("\n" + styleErr.Render("Error: "+c.Error) + "\n")
	}

	b.WriteString("\n" + styleSec.Render("── curl ") + strings.Repeat("─", max(0, width-9)) + "\n")
	b.WriteString(styleDim.Render(buildCurl(c)) + "\n")

	return b.String()
}

func renderCaptureDiff(a, b *capture.Capture, width int) string {
	var sb strings.Builder
	sb.WriteString(styleSec.Render("── Diff ") + strings.Repeat("─", max(0, width-9)) + "\n")
	sb.WriteString(styleDim.Render(fmt.Sprintf("A: %s   B: %s\n", a.ID[:8], b.ID[:8])))
	sb.WriteString("\n")

	fields := []struct {
		label string
		va    string
		vb    string
	}{
		{"Method", a.Request.Method, b.Request.Method},
		{"URL", a.Request.URL, b.Request.URL},
		{"Status", strconv.Itoa(sess.ResponseStatus(a)), strconv.Itoa(sess.ResponseStatus(b))},
		{"Duration", fmtLatency(a.Duration), fmtLatency(b.Duration)},
		{"Req body", string(a.Request.Body), string(b.Request.Body)},
		{"Resp body", func() string {
			if a.Response != nil {
				return string(a.Response.Body)
			}
			return ""
		}(), func() string {
			if b.Response != nil {
				return string(b.Response.Body)
			}
			return ""
		}()},
	}

	col := (width - 20) / 2
	if col < 10 {
		col = 10
	}
	for _, f := range fields {
		match := f.va == f.vb
		va := truncate(f.va, col)
		vb := truncate(f.vb, col)
		indicator := styleMatch.Render("✓")
		if !match {
			indicator = styleMiss.Render("✗")
		}
		line := fmt.Sprintf("  %-12s  %-*s  %-*s  %s", f.label, col, va, col, vb, indicator)
		if match {
			sb.WriteString(styleMatch.Render(line) + "\n")
		} else {
			sb.WriteString(styleMiss.Render(line) + "\n")
		}
	}
	return sb.String()
}

func renderSessionDiff(seqA, seqB []*capture.Capture, nameA, nameB string, width int) string {
	var sb strings.Builder
	sb.WriteString(styleSec.Render("── Session Diff ") + strings.Repeat("─", max(0, width-17)) + "\n")
	sb.WriteString(styleDim.Render(fmt.Sprintf("A: %s (%d)   B: %s (%d)\n\n", nameA, len(seqA), nameB, len(seqB))))

	n := len(seqA)
	if len(seqB) > n {
		n = len(seqB)
	}
	diffs := 0
	col := (width - 12) / 2
	if col < 10 {
		col = 10
	}
	for i := 0; i < n; i++ {
		var lineA, lineB string
		if i < len(seqA) {
			c := seqA[i]
			lineA = fmt.Sprintf("%s %s %d", c.Request.Method, sess.RequestPath(c), sess.ResponseStatus(c))
		}
		if i < len(seqB) {
			c := seqB[i]
			lineB = fmt.Sprintf("%s %s %d", c.Request.Method, sess.RequestPath(c), sess.ResponseStatus(c))
		}
		match := lineA == lineB
		if !match {
			diffs++
		}
		va := truncate(lineA, col)
		vb := truncate(lineB, col)
		indicator := styleMatch.Render("✓")
		if !match {
			indicator = styleMiss.Render("✗")
		}
		line := fmt.Sprintf("  [%3d]  %-*s  %-*s  %s", i+1, col, va, col, vb, indicator)
		if match {
			sb.WriteString(styleMatch.Render(line) + "\n")
		} else {
			sb.WriteString(styleMiss.Render(line) + "\n")
		}
	}
	if diffs == 0 {
		sb.WriteString("\n" + styleMatch.Render("Sessions match.") + "\n")
	} else {
		sb.WriteString(fmt.Sprintf("\n%s\n", styleMiss.Render(fmt.Sprintf("%d difference(s) found.", diffs))))
	}
	return sb.String()
}

func buildCurl(c *capture.Capture) string {
	skip := map[string]bool{
		"content-length": true, "transfer-encoding": true,
		"connection": true, "proxy-connection": true,
	}
	var sb strings.Builder
	sb.WriteString("curl -X " + c.Request.Method + " '" + strings.ReplaceAll(c.Request.URL, "'", "'\\''") + "'")
	for k, vs := range c.Request.Headers {
		if skip[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			sb.WriteString(" \\\n  -H '" + k + ": " + strings.ReplaceAll(v, "'", "'\\''") + "'")
		}
	}
	if len(c.Request.Body) > 0 {
		sb.WriteString(" \\\n  --data '" + strings.ReplaceAll(string(c.Request.Body), "'", "'\\''") + "'")
	}
	return sb.String()
}

func initMockInputs() [5]textinput.Model {
	placeholders := []string{"GET", "/api/path", "200", "application/json", `{"ok":true}`}
	var inputs [5]textinput.Model
	for i := range inputs {
		t := textinput.New()
		t.Placeholder = placeholders[i]
		t.Width = 40
		inputs[i] = t
	}
	inputs[0].Focus()
	return inputs
}

func initICInputs(pr *intercept.PendingRequest) [3]textinput.Model {
	var inputs [3]textinput.Model
	for i := range inputs {
		t := textinput.New()
		t.Width = 50
		inputs[i] = t
	}
	inputs[0].SetValue(pr.Method)
	if len(pr.Headers) > 0 {
		flat := make(map[string]string)
		for k, vs := range pr.Headers {
			if len(vs) > 0 {
				flat[k] = vs[0]
			}
		}
		if data, err := json.Marshal(flat); err == nil {
			inputs[1].SetValue(string(data))
		}
	}
	inputs[2].SetValue(pr.Body)
	inputs[0].Placeholder = "GET"
	inputs[1].Placeholder = `{"Authorization":"Bearer ..."}`
	inputs[2].Placeholder = "request body"
	inputs[0].Focus()
	return inputs
}

func initReplayInputs(c *capture.Capture) [4]textinput.Model {
	var inputs [4]textinput.Model
	widths := []int{10, 60, 50, 50}
	placeholders := []string{"GET", "https://...", `{"Authorization":"Bearer ..."}`, "request body"}
	for i := range inputs {
		t := textinput.New()
		t.Width = widths[i]
		t.Placeholder = placeholders[i]
		inputs[i] = t
	}
	inputs[0].SetValue(c.Request.Method)
	inputs[1].SetValue(c.Request.URL)
	flat := make(map[string]string)
	for k, vs := range c.Request.Headers {
		if len(vs) > 0 {
			flat[k] = vs[0]
		}
	}
	if data, err := json.Marshal(flat); err == nil {
		inputs[2].SetValue(string(data))
	}
	inputs[3].SetValue(string(c.Request.Body))
	inputs[0].Focus()
	return inputs
}

func initMockInputsFromCapture(c *capture.Capture) [5]textinput.Model {
	inputs := initMockInputs()
	inputs[0].SetValue(c.Request.Method)
	u, err := url.Parse(c.Request.URL)
	if err == nil {
		inputs[1].SetValue(u.Path)
	} else {
		inputs[1].SetValue(c.Request.URL)
	}
	if c.Response != nil {
		inputs[2].SetValue(strconv.Itoa(c.Response.StatusCode))
		if ct := c.Response.Headers.Get("Content-Type"); ct != "" {
			inputs[3].SetValue(ct)
		}
		inputs[4].SetValue(string(c.Response.Body))
	}
	return inputs
}

func replayCapture(c *capture.Capture, proxyURL string) {
	req, err := http.NewRequest(c.Request.Method, c.Request.URL, bytes.NewReader(c.Request.Body))
	if err != nil {
		return
	}
	for k, vals := range c.Request.Headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	var transport http.RoundTripper
	if proxyURL != "" {
		if pu, err := url.Parse(proxyURL); err == nil {
			transport = &http.Transport{Proxy: http.ProxyURL(pu)}
		}
	}
	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
}

func wsOpcodeName(op int) string {
	switch op {
	case 0:
		return "continuation"
	case 1:
		return "text"
	case 2:
		return "binary"
	case 8:
		return "close"
	case 9:
		return "ping"
	case 10:
		return "pong"
	default:
		return fmt.Sprintf("op%d", op)
	}
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
