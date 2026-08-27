package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
)

const (
	leftPanelRatio = 0.32
	pollInterval   = time.Second
	headerHeight   = 1
	footerHeight   = 1
)

// Model is the top-level bubbletea model.
type Model struct {
	sockPath string
	dbPath   string

	// data
	projects  []protocol.Project
	worktrees map[string][]protocol.Worktree
	sessions  map[string][]protocol.Session

	// tree
	items    []treeItem
	cursor   int
	expanded map[string]bool

	// right panel
	output   string
	viewport viewport.Model

	// layout
	width, height int
	ready         bool
	rightFocused  bool // true = j/k scroll right pane, false = navigate tree

	jumpToSession   bool // set after n key; auto-navigate to first new session
	sessionLocked   bool // when true, keys are forwarded to the active PTY
	lockedSessionID string
	daemonDown      bool
	err             string
}

// New creates a new TUI model.
func New(sockPath, dbPath string) Model {
	return Model{
		sockPath:  sockPath,
		dbPath:    dbPath,
		worktrees: make(map[string][]protocol.Worktree),
		sessions:  make(map[string][]protocol.Session),
		expanded:  make(map[string]bool),
	}
}

// --- tea.Msg types ---

type tickMsg time.Time
type fastTickMsg time.Time
type dataMsg daemonData
type snapshotMsg string
type sessionCreatedMsg string // session ID
type errMsg string
type daemonDownMsg struct{}

// --- Init ---

func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchDataCmd(m.dbPath), tickCmd())
}

// --- Update ---

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.rebuildViewport()
		m.ready = true
		// Resize all active sessions to match the right panel.
		rows, cols := m.panelSize()
		for _, wtSessions := range m.sessions {
			for _, sess := range wtSessions {
				cmds = append(cmds, resizeSessionCmd(m.sockPath, sess.ID, rows, cols))
			}
		}

	case tickMsg:
		cmds = append(cmds, fetchDataCmd(m.dbPath), tickCmd())
		if m.sessionLocked {
			cmds = append(cmds, fetchSnapshotCmd(m.sockPath, m.lockedSessionID), fastTickCmd())
		} else if sess := selectedSession(m.items, m.cursor); sess != nil {
			cmds = append(cmds, fetchSnapshotCmd(m.sockPath, sess.ID))
		}

	case fastTickMsg:
		if m.sessionLocked {
			cmds = append(cmds, fetchSnapshotCmd(m.sockPath, m.lockedSessionID), fastTickCmd())
		}

	case dataMsg:
		m.err = ""
		m.projects = msg.projects
		m.worktrees = msg.worktrees
		m.sessions = msg.sessions
		if len(m.expanded) == 0 {
			for _, p := range m.projects {
				m.expanded["p:"+p.ID] = true
				for _, wt := range m.worktrees[p.RepoPath] {
					m.expanded["w:"+wt.ID] = true
				}
			}
		}
		m.rebuildItems()
		m.clampCursor()
		if m.jumpToSession {
			if idx := firstSessionIndex(m.items); idx >= 0 {
				m.cursor = idx
				m.jumpToSession = false
				m.refreshSnapshot(&cmds)
			}
		}

	case sessionCreatedMsg:
		m.sessionLocked = true
		m.lockedSessionID = string(msg)
		m.jumpToSession = true
		cmds = append(cmds, fetchDataCmd(m.dbPath), fastTickCmd())

	case snapshotMsg:
		if string(msg) != "" {
			m.output = string(msg)
			m.viewport.SetContent(m.output)
		}

	case daemonDownMsg:
		m.daemonDown = true

	case errMsg:
		m.err = string(msg)

	case tea.KeyMsg:
		if m.sessionLocked {
			return m.handleLockedKey(msg, cmds)
		}
		return m.handleKey(msg, cmds)
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		if !m.rightFocused {
			// Entering right panel: if a session is selected, lock for typing.
			if m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor].kind == kindSession {
				sess := m.items[m.cursor].session
				m.sessionLocked = true
				m.lockedSessionID = sess.ID
				cmds = append(cmds, fastTickCmd())
			} else {
				m.rightFocused = true
			}
		} else {
			m.rightFocused = false
		}

	case "j", "down":
		if m.rightFocused {
			m.viewport.LineDown(1)
		} else {
			m.cursor++
			m.clampCursor()
			m.refreshSnapshot(&cmds)
		}

	case "k", "up":
		if m.rightFocused {
			m.viewport.LineUp(1)
		} else {
			if m.cursor > 0 {
				m.cursor--
			}
			m.refreshSnapshot(&cmds)
		}

	case " ", "enter":
		if m.rightFocused {
			break
		}
		m.toggleOrAttach(&cmds)

	case "a":
		cwd, _ := os.Getwd()
		return m, tea.ExecProcess(
			exec.Command(executablePath(), "project", "add", cwd),
			func(err error) tea.Msg { return fetchDataCmd(m.dbPath)() },
		)

	case "n":
		if cwd := m.selectedCWD(); cwd != "" {
			rows, cols := m.panelSize()
			return m, createSessionCmd(m.sockPath, cwd, rows, cols)
		}

	case "x":
		if sess := selectedSession(m.items, m.cursor); sess != nil {
			cmds = append(cmds, killSessionCmd(m.sockPath, m.dbPath, sess.ID, sess.PID))
		}

	case "r":
		cmds = append(cmds, fetchDataCmd(m.dbPath))
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) toggleOrAttach(cmds *[]tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return
	}
	item := m.items[m.cursor]

	switch item.kind {
	case kindProject:
		m.expanded["p:"+item.project.ID] = !m.expanded["p:"+item.project.ID]
		m.rebuildItems()
		m.clampCursor()

	case kindWorktree:
		m.expanded["w:"+item.worktree.ID] = !m.expanded["w:"+item.worktree.ID]
		m.rebuildItems()
		m.clampCursor()

	case kindSession:
		m.sessionLocked = true
		m.lockedSessionID = item.session.ID
		*cmds = append(*cmds, fastTickCmd())
	}
}

func (m Model) handleLockedKey(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	// Ctrl+Q escapes back to navigation mode.
	if msg.Type == tea.KeyCtrlQ {
		m.sessionLocked = false
		m.lockedSessionID = ""
		m.rightFocused = false
		return m, tea.Batch(cmds...)
	}
	data := keyToBytes(msg)
	if len(data) > 0 {
		cmds = append(cmds, sendInputCmd(m.sockPath, m.lockedSessionID, data))
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) refreshSnapshot(cmds *[]tea.Cmd) {
	if sess := selectedSession(m.items, m.cursor); sess != nil {
		rows, cols := m.panelSize()
		*cmds = append(*cmds,
			resizeSessionCmd(m.sockPath, sess.ID, rows, cols),
			fetchSnapshotCmd(m.sockPath, sess.ID),
		)
	} else {
		m.output = ""
		m.viewport.SetContent("")
	}
}

// --- View ---

func (m Model) View() string {
	if !m.ready {
		return "Loading…\n"
	}
	if m.daemonDown {
		return lipgloss.JoinVertical(lipgloss.Left,
			styleHeader.Width(m.width).Render("canopy"),
			"\n",
			styleOutputEmpty.Render("  Daemon is not running.\n\n  Start it with:\n\n    canopy daemon start"),
			"\n",
			styleFooter.Width(m.width).Render(styleFooterKey.Render("q")+" quit"),
		)
	}
	if m.err != "" {
		return fmt.Sprintf("Error: %s\nPress q to quit.\n", m.err)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		m.renderBody(),
		m.renderFooter(),
	)
}

func (m Model) renderHeader() string {
	crumb := breadcrumb(m.items, m.cursor)
	sess := selectedSession(m.items, m.cursor)

	var right string
	if sess != nil {
		right = stateDot(sess.State) + " " + stateLabel(sess.State)
	}

	left := styleHeaderBreadcrumb.Render(crumb)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 0 {
		gap = 0
	}
	return styleHeader.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) renderFooter() string {
	var hints []string
	if m.sessionLocked {
		hints = []string{
			styleFooterKey.Render("ctrl+q") + " back to tree",
			"  typing goes to Claude",
		}
	} else if len(m.projects) == 0 {
		hints = []string{
			styleFooterKey.Render("a") + " add project",
			styleFooterKey.Render("q") + " quit",
		}
	} else {
		paneHint := styleFooterKey.Render("tab") + " → output"
		if m.rightFocused {
			paneHint = styleFooterKey.Render("tab") + " → tree"
		}
		hints = []string{
			styleFooterKey.Render("enter") + " expand/attach",
			styleFooterKey.Render("n") + " new session",
			styleFooterKey.Render("a") + " add project",
			styleFooterKey.Render("x") + " kill",
			paneHint,
			styleFooterKey.Render("q") + " quit",
		}
	}
	return styleFooter.Width(m.width).Render(strings.Join(hints, "   "))
}

func (m Model) renderBody() string {
	bodyH := m.height - headerHeight - footerHeight
	if bodyH < 1 {
		bodyH = 1
	}

	if len(m.projects) == 0 {
		return m.renderEmptyState(bodyH)
	}

	leftW := int(float64(m.width) * leftPanelRatio)
	if leftW < 22 {
		leftW = 22
	}
	rightW := m.width - leftW - 1

	treeH := bodyH - 2
	if treeH < 1 {
		treeH = 1
	}

	rightActive := m.rightFocused || m.sessionLocked

	// PROJECTS title is blue when tree has focus; divider is blue when right has focus.
	titleStyle := stylePanelTitle
	if !rightActive {
		titleStyle = stylePanelTitle.Foreground(colorSelected)
	}

	left := titleStyle.Width(leftW).Render("PROJECTS") + "\n" +
		renderTree(m.items, m.cursor, leftW, treeH)

	divLines := make([]string, bodyH)
	for i := range divLines {
		divLines[i] = "│"
	}
	divColor := colorBorder
	if rightActive {
		divColor = colorSelected
	}
	divider := styleDivider.Foreground(divColor).Render(strings.Join(divLines, "\n"))

	var right string
	if m.output == "" {
		right = m.renderDetail(rightW, bodyH)
	} else {
		right = m.viewport.View()
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
}

func (m Model) renderEmptyState(bodyH int) string {
	pad := strings.Repeat("\n", bodyH/3)
	content := pad +
		lipgloss.NewStyle().Foreground(colorText).Bold(true).PaddingLeft(4).Render("No projects yet.") + "\n\n" +
		styleOutputEmpty.Render("Press  a  to add the current directory as a project.") + "\n" +
		styleOutputEmpty.Render("Or run: canopy project add")
	return lipgloss.NewStyle().Width(m.width).Height(bodyH).Render(content)
}

func (m Model) renderDetail(width, height int) string {
	sess := selectedSession(m.items, m.cursor)
	if sess == nil {
		return styleOutputEmpty.Width(width).Render(
			"Select a session to preview output.\nPress n to start a new session.",
		)
	}

	title := sess.Title
	if title == "" {
		title = sess.CWD
	}
	age := time.Since(sess.StartedAt).Round(time.Second)

	lines := []string{
		"",
		lipgloss.NewStyle().Foreground(colorText).Bold(true).PaddingLeft(2).Render(title),
		"",
		lipgloss.NewStyle().PaddingLeft(2).Render(stateDot(sess.State) + "  " + stateLabel(sess.State)),
		"",
		styleOutputEmpty.Render("tool     " + sess.Tool),
		styleOutputEmpty.Render("started  " + age.String() + " ago"),
		styleOutputEmpty.Render("cwd      " + sess.CWD),
		"",
		styleOutputEmpty.Render("press enter to attach"),
	}
	_ = height
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

// --- helpers ---

func (m *Model) rebuildItems() {
	m.items = buildTree(m.projects, m.worktrees, m.sessions, m.expanded)
}

func (m *Model) clampCursor() {
	if len(m.items) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) rebuildViewport() {
	bodyH := m.height - headerHeight - footerHeight
	leftW := int(float64(m.width) * leftPanelRatio)
	rightW := m.width - leftW - 1
	if rightW < 10 {
		rightW = 10
	}
	m.viewport = viewport.New(rightW, bodyH)
	m.viewport.SetContent(m.output)
}

// panelSize returns the (rows, cols) of the right panel in terminal cells.
func (m *Model) panelSize() (uint16, uint16) {
	bodyH := m.height - headerHeight - footerHeight
	leftW := int(float64(m.width) * leftPanelRatio)
	rightW := m.width - leftW - 1
	if rightW < 10 {
		rightW = 10
	}
	if bodyH < 1 {
		bodyH = 1
	}
	return uint16(bodyH), uint16(rightW)
}

func (m *Model) selectedCWD() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	item := m.items[m.cursor]
	if item.worktree != nil {
		return item.worktree.Path
	}
	if item.session != nil {
		return item.session.CWD
	}
	return ""
}

func executablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "canopy"
	}
	return exe
}

// --- tea.Cmd constructors ---

func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fastTickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return fastTickMsg(t)
	})
}

func fetchDataCmd(dbPath string) tea.Cmd {
	return func() tea.Msg {
		data, err := fetchAll(dbPath)
		if err != nil {
			return errMsg(err.Error())
		}
		return dataMsg(data)
	}
}

func fetchSnapshotCmd(sockPath, sessionID string) tea.Cmd {
	return func() tea.Msg {
		text, err := fetchSnapshot(sockPath, sessionID)
		if err != nil {
			if isDaemonDown(err) {
				return daemonDownMsg{}
			}
			return snapshotMsg("")
		}
		return snapshotMsg(text)
	}
}


func resizeSessionCmd(sockPath, sessionID string, rows, cols uint16) tea.Cmd {
	return func() tea.Msg {
		p, _ := json.Marshal(protocol.ResizeSessionParams{SessionID: sessionID, Rows: rows, Cols: cols})
		_, _ = rpc(sockPath, protocol.Cmd{Type: protocol.CmdResizeSession, Payload: p})
		return nil
	}
}

func createSessionCmd(sockPath, cwd string, rows, cols uint16) tea.Cmd {
	return func() tea.Msg {
		p, _ := json.Marshal(protocol.NewSessionParams{
			Tool: "claude",
			CWD:  cwd,
			Rows: rows,
			Cols: cols,
		})
		raw, err := rpc(sockPath, protocol.Cmd{Type: protocol.CmdNewSession, Payload: p})
		if err != nil {
			if isDaemonDown(err) {
				return daemonDownMsg{}
			}
			return errMsg(err.Error())
		}
		var sess protocol.Session
		if err := json.Unmarshal(raw, &sess); err != nil {
			return errMsg("parse session: " + err.Error())
		}
		return sessionCreatedMsg(sess.ID)
	}
}

func sendInputCmd(sockPath, sessionID string, data []byte) tea.Cmd {
	return func() tea.Msg {
		p, _ := json.Marshal(protocol.InputParams{SessionID: sessionID, Data: data})
		_, _ = rpc(sockPath, protocol.Cmd{Type: protocol.CmdInput, Payload: p})
		return nil
	}
}

func killSessionCmd(sockPath, dbPath, sessionID string, pid int) tea.Cmd {
	return func() tea.Msg {
		p, _ := json.Marshal(protocol.KillSessionParams{SessionID: sessionID})
		_, _ = rpc(sockPath, protocol.Cmd{Type: protocol.CmdKillSession, Payload: p})

		// Also clean up in DB in case daemon wasn't tracking it.
		db, err := store.Open(dbPath)
		if err == nil {
			defer db.Close()
			if sess, err := db.GetSession(sessionID); err == nil {
				sess.State = protocol.StateTerminated
				sess.Archived = true
				_ = db.UpdateSession(sess)
			}
		}
		return fetchDataCmd(dbPath)()
	}
}
