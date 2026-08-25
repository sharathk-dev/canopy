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

	// data
	projects  []protocol.Project
	worktrees map[string][]protocol.Worktree
	sessions  map[string][]protocol.Session

	// tree
	items    []treeItem
	cursor   int
	expanded map[string]bool // "p:<id>" | "w:<id>" → expanded

	// right panel
	output   string
	viewport viewport.Model

	// layout
	width, height int
	ready         bool

	err string
}

// New creates a new TUI model.
func New(sockPath string) Model {
	return Model{
		sockPath:  sockPath,
		worktrees: make(map[string][]protocol.Worktree),
		sessions:  make(map[string][]protocol.Session),
		expanded:  make(map[string]bool),
	}
}

// --- tea.Msg types ---

type tickMsg time.Time
type dataMsg daemonData
type snapshotMsg string
type errMsg string

// --- Init ---

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		fetchDataCmd(m.sockPath),
		tickCmd(),
	)
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

	case tickMsg:
		cmds = append(cmds, fetchDataCmd(m.sockPath), tickCmd())
		if sess := selectedSession(m.items, m.cursor); sess != nil {
			cmds = append(cmds, fetchSnapshotCmd(m.sockPath, sess.ID))
		}

	case dataMsg:
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

	case snapshotMsg:
		m.output = string(msg)
		m.viewport.SetContent(m.output)

	case errMsg:
		m.err = string(msg)

	case tea.KeyMsg:
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

	case "j", "down":
		m.cursor++
		m.clampCursor()
		m.refreshSnapshot(&cmds)

	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		m.refreshSnapshot(&cmds)

	case " ", "enter":
		m.toggleOrAttach(&cmds)

	case "n":
		if cwd := m.selectedWorktreeCWD(); cwd != "" {
			return m, tea.ExecProcess(
				exec.Command(executablePath(), "session", "new", "--tool=claude", "--cwd="+cwd),
				func(err error) tea.Msg { return fetchDataCmd(m.sockPath)() },
			)
		}

	case "x":
		if sess := selectedSession(m.items, m.cursor); sess != nil {
			cmds = append(cmds, killSessionCmd(m.sockPath, sess.ID))
		}

	case "r":
		cmds = append(cmds, fetchDataCmd(m.sockPath))
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
		key := "p:" + item.project.ID
		m.expanded[key] = !m.expanded[key]
		m.rebuildItems()
		m.clampCursor()

	case kindWorktree:
		key := "w:" + item.worktree.ID
		m.expanded[key] = !m.expanded[key]
		m.rebuildItems()
		m.clampCursor()

	case kindSession:
		sess := item.session
		attachCmd := exec.Command(executablePath(), "session", "attach", sess.ID)
		attachCmd.Stdin = os.Stdin
		attachCmd.Stdout = os.Stdout
		attachCmd.Stderr = os.Stderr
		*cmds = append(*cmds, tea.ExecProcess(attachCmd, func(err error) tea.Msg {
			return fetchDataCmd(m.sockPath)()
		}))
	}
}

func (m *Model) refreshSnapshot(cmds *[]tea.Cmd) {
	if sess := selectedSession(m.items, m.cursor); sess != nil {
		*cmds = append(*cmds, fetchSnapshotCmd(m.sockPath, sess.ID))
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
	if m.err != "" {
		return fmt.Sprintf("Error: %s\nPress q to quit.\n", m.err)
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	body := m.renderBody()

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m Model) renderHeader() string {
	crumb := breadcrumb(m.items, m.cursor)
	sess := selectedSession(m.items, m.cursor)

	var right string
	if sess != nil {
		right = stateDot(sess.State) + " " + stateLabel(sess.State)
	}

	left := styleHeaderBreadcrumb.Render(crumb)
	rightW := lipgloss.Width(right)
	leftW := lipgloss.Width(left)
	gap := m.width - leftW - rightW - 2
	if gap < 0 {
		gap = 0
	}

	return styleHeader.Width(m.width).Render(
		left + strings.Repeat(" ", gap) + right,
	)
}

func (m Model) renderFooter() string {
	hints := []string{
		styleFooterKey.Render("enter") + " attach",
		styleFooterKey.Render("n") + " new session",
		styleFooterKey.Render("x") + " kill",
		styleFooterKey.Render("r") + " refresh",
		styleFooterKey.Render("q") + " quit",
	}
	return styleFooter.Width(m.width).Render(strings.Join(hints, "   "))
}

func (m Model) renderBody() string {
	bodyH := m.height - headerHeight - footerHeight
	if bodyH < 1 {
		bodyH = 1
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

	left := stylePanelTitle.Width(leftW).Render("PROJECTS") + "\n" +
		renderTree(m.items, m.cursor, leftW, treeH) +
		"\n" + styleOutputEmpty.Render("+ n new session")

	divLines := make([]string, bodyH)
	for i := range divLines {
		divLines[i] = "│"
	}
	divider := styleDivider.Render(strings.Join(divLines, "\n"))

	var right string
	if m.output == "" {
		right = styleOutputEmpty.Width(rightW).Render(
			"Select a session to preview output.\nPress enter to attach.",
		)
	} else {
		right = styleOutput.Width(rightW).Render(m.viewport.View())
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
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

func (m *Model) selectedWorktreeCWD() string {
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

func fetchDataCmd(sockPath string) tea.Cmd {
	return func() tea.Msg {
		data, err := fetchAll(sockPath)
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
			return snapshotMsg("")
		}
		return snapshotMsg(text)
	}
}

func killSessionCmd(sockPath, sessionID string) tea.Cmd {
	return func() tea.Msg {
		p, _ := json.Marshal(protocol.KillSessionParams{SessionID: sessionID})
		_, _ = rpc(sockPath, protocol.Cmd{Type: protocol.CmdKillSession, Payload: p})
		return fetchDataCmd(sockPath)()
	}
}
