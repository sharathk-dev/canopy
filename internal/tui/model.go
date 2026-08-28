package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/scheduler"
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
	schedules []protocol.Schedule
	runs      map[string][]protocol.ScheduleRun

	// config
	config protocol.Config

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

	jumpToSession       bool // set after n key; auto-navigate to first new session
	sessionLocked       bool // when true, keys are forwarded to the active PTY
	lockedSessionID     string
	confirmKillID       string
	titleEditingID      string
	titleInput          string
	newSessionCWD       string
	projectAdding       bool
	projectPath         string
	worktreeAdding      bool
	worktreeRepo        string
	worktreeBranch      string
	worktreePath        string
	worktreePathMode    bool
	projectDeleteID     string
	projectDeleteName   string
	projectDeleteInput  string
	worktreeDeleteID    string
	worktreeDeleteRepo  string
	worktreeDeletePath  string
	worktreeDeleteInput string
	scheduleAdding      bool
	scheduleField       int
	scheduleName        string
	scheduleCron        string
	scheduleSkill       string
	searching           bool
	searchInput         string
	searchQuery         string
	searchPrevious      string
	showHelp            bool
	daemonDown          bool
	status              string
	sessionsSized       bool
	err                 string
}

// New creates a new TUI model.
func New(sockPath, dbPath string) Model {
	return Model{
		sockPath:  sockPath,
		dbPath:    dbPath,
		worktrees: make(map[string][]protocol.Worktree),
		sessions:  make(map[string][]protocol.Session),
		runs:      make(map[string][]protocol.ScheduleRun),
		expanded:  make(map[string]bool),
	}
}

// --- tea.Msg types ---

type tickMsg time.Time
type fastTickMsg time.Time
type animationTickMsg struct{}
type dataMsg daemonData
type snapshotMsg string
type sessionCreatedMsg string // session ID
type errMsg string
type daemonDownMsg struct{}
type copiedMsg struct{}
type clearStatusMsg struct{}

// --- Init ---

func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchDataCmd(m.dbPath), tickCmd(), animationTickCmd())
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
		m.sessionsSized = false
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

	case animationTickMsg:
		animationFrame++
		cmds = append(cmds, animationTickCmd())

	case dataMsg:
		m.err = ""
		m.projects = msg.projects
		m.worktrees = msg.worktrees
		m.sessions = msg.sessions
		m.schedules = msg.schedules
		m.runs = msg.runs
		m.config = msg.config
		if m.ready && !m.sessionsSized {
			rows, cols := m.panelSize()
			for _, wtSessions := range m.sessions {
				for _, sess := range wtSessions {
					cmds = append(cmds, resizeSessionCmd(m.sockPath, sess.ID, rows, cols))
				}
			}
			m.sessionsSized = true
		}
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
		if selectedSession(m.items, m.cursor) == nil {
			m.output = ""
			m.viewport.SetContent("")
		}
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

	case copiedMsg:
		m.status = "copied"
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{} }))

	case clearStatusMsg:
		m.status = ""

	case errMsg:
		m.err = string(msg)

	case tea.KeyMsg:
		if m.searching {
			return m.handleKey(msg, cmds)
		}
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
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	if m.showHelp {
		switch msg.String() {
		case "esc", "?", "q":
			m.showHelp = false
		}
		return m, tea.Batch(cmds...)
	}

	if m.projectDeleteID != "" {
		switch msg.String() {
		case "enter":
			if m.projectDeleteInput == "DELETE" {
				id := m.projectDeleteID
				m.projectDeleteID = ""
				m.projectDeleteName = ""
				m.projectDeleteInput = ""
				cmds = append(cmds, deleteProjectCmd(m.dbPath, id))
			}
		case "esc":
			m.projectDeleteID = ""
			m.projectDeleteName = ""
			m.projectDeleteInput = ""
		case "backspace", "ctrl+h":
			if len(m.projectDeleteInput) > 0 {
				runes := []rune(m.projectDeleteInput)
				m.projectDeleteInput = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) > 0 {
				m.projectDeleteInput += string(msg.Runes)
			}
		}
		return m, tea.Batch(cmds...)
	}

	if m.worktreeDeleteID != "" {
		switch msg.String() {
		case "enter":
			if m.worktreeDeleteInput == "DELETE" {
				repo := m.worktreeDeleteRepo
				path := m.worktreeDeletePath
				m.worktreeDeleteID = ""
				m.worktreeDeleteRepo = ""
				m.worktreeDeletePath = ""
				m.worktreeDeleteInput = ""
				return m, tea.ExecProcess(
					exec.Command(executablePath(), "worktree", "remove", path, "--repo", repo),
					func(error) tea.Msg { return fetchDataCmd(m.dbPath)() },
				)
			}
		case "esc":
			m.worktreeDeleteID = ""
			m.worktreeDeleteRepo = ""
			m.worktreeDeletePath = ""
			m.worktreeDeleteInput = ""
		case "backspace", "ctrl+h":
			if len(m.worktreeDeleteInput) > 0 {
				runes := []rune(m.worktreeDeleteInput)
				m.worktreeDeleteInput = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) > 0 {
				m.worktreeDeleteInput += string(msg.Runes)
			}
		}
		return m, tea.Batch(cmds...)
	}

	if m.scheduleAdding {
		fields := []*string{&m.scheduleName, &m.scheduleCron, &m.scheduleSkill}
		switch msg.String() {
		case "enter":
			if strings.TrimSpace(*fields[m.scheduleField]) == "" {
				return m, tea.Batch(cmds...)
			}
			if m.scheduleField < len(fields)-1 {
				m.scheduleField++
				return m, tea.Batch(cmds...)
			}
			name, cron, skill := strings.TrimSpace(m.scheduleName), strings.TrimSpace(m.scheduleCron), strings.TrimSpace(m.scheduleSkill)
			cwd := m.selectedCWD()
			m.scheduleAdding = false
			m.scheduleField = 0
			m.scheduleName, m.scheduleCron, m.scheduleSkill = "", "", ""
			return m, tea.Batch(createScheduleCmd(m.dbPath, name, cron, skill, cwd))
		case "esc":
			m.scheduleAdding = false
			m.scheduleField = 0
			m.scheduleName, m.scheduleCron, m.scheduleSkill = "", "", ""
		case "backspace", "ctrl+h":
			field := fields[m.scheduleField]
			if len(*field) > 0 {
				runes := []rune(*field)
				*field = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) > 0 {
				*fields[m.scheduleField] += string(msg.Runes)
			}
		}
		return m, tea.Batch(cmds...)
	}

	if m.searching {
		if msg.Type == tea.KeyEscape {
			m.searchQuery = m.searchPrevious
			m.searching = false
			m.searchInput = ""
			m.rebuildItems()
			m.clampCursor()
			return m, tea.Batch(cmds...)
		}
		if msg.Type == tea.KeyEnter {
			selectedKey := treeItemKey(m.items, m.cursor)
			m.searching = false
			m.searchInput = ""
			m.searchQuery = ""
			m.rebuildItems()
			m.cursor = findTreeItem(m.items, selectedKey)
			m.clampCursor()
			return m, tea.Batch(cmds...)
		}
		switch msg.String() {
		case "enter":
			selectedKey := treeItemKey(m.items, m.cursor)
			m.searching = false
			m.searchInput = ""
			m.searchQuery = ""
			m.rebuildItems()
			m.cursor = findTreeItem(m.items, selectedKey)
			m.clampCursor()
		case "esc":
			m.searchQuery = m.searchPrevious
			m.searching = false
			m.searchInput = ""
			m.rebuildItems()
			m.clampCursor()
		case "tab":
			m.cursor = nextSearchMatch(m.items, m.cursor)
		case "shift+tab":
			m.cursor = prevSearchMatch(m.items, m.cursor)
		case "backspace", "ctrl+h":
			if len(m.searchInput) > 0 {
				runes := []rune(m.searchInput)
				m.searchInput = string(runes[:len(runes)-1])
			}
			m.searchQuery = strings.TrimSpace(m.searchInput)
			m.rebuildItems()
			m.cursor = searchResultCursor(m.items, m.searchQuery)
		default:
			if len(msg.Runes) > 0 {
				m.searchInput += string(msg.Runes)
			}
			m.searchQuery = strings.TrimSpace(m.searchInput)
			m.rebuildItems()
			m.cursor = searchResultCursor(m.items, m.searchQuery)
		}
		return m, tea.Batch(cmds...)
	}

	if m.projectAdding {
		switch msg.String() {
		case "enter":
			path := strings.TrimSpace(m.projectPath)
			if path != "" {
				m.projectAdding = false
				m.projectPath = ""
				return m, tea.ExecProcess(
					exec.Command(executablePath(), "project", "add", path),
					func(error) tea.Msg { return fetchDataCmd(m.dbPath)() },
				)
			}
		case "esc":
			m.projectAdding = false
			m.projectPath = ""
		case "backspace", "ctrl+h":
			if len(m.projectPath) > 0 {
				runes := []rune(m.projectPath)
				m.projectPath = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) > 0 {
				m.projectPath += string(msg.Runes)
			}
		}
		return m, tea.Batch(cmds...)
	}

	if m.worktreeAdding {
		switch msg.String() {
		case "enter":
			if !m.worktreePathMode {
				if strings.TrimSpace(m.worktreeBranch) != "" {
					m.worktreePathMode = true
				}
				return m, tea.Batch(cmds...)
			}
			branch := strings.TrimSpace(m.worktreeBranch)
			if branch == "" {
				return m, tea.Batch(cmds...)
			}
			repo := m.worktreeRepo
			path := strings.TrimSpace(m.worktreePath)
			m.worktreeAdding = false
			m.worktreeRepo = ""
			m.worktreeBranch = ""
			m.worktreePath = ""
			m.worktreePathMode = false
			args := []string{"worktree", "add", branch, "--repo", repo}
			if path != "" {
				args = append(args, "--path", path)
			}
			return m, tea.ExecProcess(exec.Command(executablePath(), args...),
				func(error) tea.Msg { return fetchDataCmd(m.dbPath)() })
		case "esc":
			m.worktreeAdding = false
			m.worktreeRepo = ""
			m.worktreeBranch = ""
			m.worktreePath = ""
			m.worktreePathMode = false
		case "backspace", "ctrl+h":
			field := &m.worktreeBranch
			if m.worktreePathMode {
				field = &m.worktreePath
			}
			if len(*field) > 0 {
				runes := []rune(*field)
				*field = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) > 0 {
				if m.worktreePathMode {
					m.worktreePath += string(msg.Runes)
				} else {
					m.worktreeBranch += string(msg.Runes)
				}
			}
		}
		return m, tea.Batch(cmds...)
	}

	if m.newSessionCWD != "" {
		switch msg.String() {
		case "enter":
			cwd := m.newSessionCWD
			title := strings.TrimSpace(m.titleInput)
			if title == "" {
				title = "session_" + protocol.NewID()[:5]
			}
			m.newSessionCWD = ""
			m.titleInput = ""
			rows, cols := m.panelSize()
			cmds = append(cmds, createSessionCmd(m.sockPath, cwd, title, rows, cols))
		case "esc":
			m.newSessionCWD = ""
			m.titleInput = ""
		case "backspace", "ctrl+h":
			if len(m.titleInput) > 0 {
				runes := []rune(m.titleInput)
				m.titleInput = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) > 0 {
				m.titleInput += string(msg.Runes)
			}
		}
		return m, tea.Batch(cmds...)
	}

	if m.titleEditingID != "" {
		switch msg.String() {
		case "enter":
			if title := strings.TrimSpace(m.titleInput); title != "" {
				cmds = append(cmds, updateTitleCmd(m.sockPath, m.titleEditingID, title), fetchDataCmd(m.dbPath))
				m.titleEditingID = ""
				m.titleInput = ""
			}
		case "esc":
			m.titleEditingID = ""
			m.titleInput = ""
		case "backspace", "ctrl+h":
			if len(m.titleInput) > 0 {
				runes := []rune(m.titleInput)
				m.titleInput = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) > 0 {
				m.titleInput += string(msg.Runes)
			}
		}
		return m, tea.Batch(cmds...)
	}

	if m.confirmKillID != "" {
		switch msg.String() {
		case "y", "enter":
			id := m.confirmKillID
			m.confirmKillID = ""
			cmds = append(cmds, killSessionCmd(m.sockPath, m.dbPath, id, 0))
		case "n", "esc":
			m.confirmKillID = ""
		}
		return m, tea.Batch(cmds...)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "/":
		m.searching = true
		m.searchPrevious = m.searchQuery
		m.searchInput = m.searchQuery

	case "?":
		m.showHelp = true

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

	case " ":
		if schedule := selectedSchedule(m.items, m.cursor); schedule != nil {
			if schedule.Enabled {
				m.status = "disabled"
			} else {
				m.status = "enabled"
			}
			cmds = append(cmds, toggleScheduleCmd(m.dbPath, *schedule), clearStatusCmd())
			break
		}
		if m.rightFocused {
			break
		}
		m.toggleOrAttach(&cmds)

	case "enter":
		if m.rightFocused {
			break
		}
		m.toggleOrAttach(&cmds)

	case "a":
		m.projectAdding = true
		m.projectPath = ""

	case "w":
		if repo := m.selectedRepoPath(); repo != "" {
			m.worktreeAdding = true
			m.worktreeRepo = repo
			m.worktreeBranch = ""
			m.worktreePath = ""
			m.worktreePathMode = false
		}

	case "s":
		m.scheduleAdding = true
		m.scheduleField = 0
		m.scheduleName, m.scheduleCron, m.scheduleSkill = "", "", ""

	case "r":
		if schedule := selectedSchedule(m.items, m.cursor); schedule != nil {
			m.status = "running"
			cmds = append(cmds, runScheduleCmd(m.sockPath, m.dbPath, schedule.ID), clearStatusCmd())
		} else {
			cmds = append(cmds, fetchDataCmd(m.dbPath))
		}

	case "c":
		output := m.output
		if schedule := selectedSchedule(m.items, m.cursor); schedule != nil {
			if runs := m.runs[schedule.ID]; len(runs) > 0 {
				output = runs[0].Output
			}
		}
		if output != "" && (m.rightFocused || selectedSchedule(m.items, m.cursor) != nil) {
			cmds = append(cmds, copyOutputCmd(output))
		}

	case "n":
		if cwd := m.selectedCWD(); cwd != "" {
			m.newSessionCWD = cwd
			m.titleInput = ""
		}

	case "x":
		if m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor].kind == kindProject {
			project := m.items[m.cursor].project
			m.projectDeleteID = project.ID
			m.projectDeleteName = project.Name
			m.projectDeleteInput = ""
		} else if m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor].kind == kindWorktree {
			item := m.items[m.cursor]
			m.worktreeDeleteID = item.worktree.ID
			m.worktreeDeleteRepo = item.project.RepoPath
			m.worktreeDeletePath = item.worktree.Path
			m.worktreeDeleteInput = ""
		} else if sess := selectedSession(m.items, m.cursor); sess != nil {
			m.confirmKillID = sess.ID
		}

	case "e":
		if sess := selectedSession(m.items, m.cursor); sess != nil {
			m.titleEditingID = sess.ID
			m.titleInput = sess.Title
		}

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
	if m.showHelp {
		return m.renderHelp()
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
	if m.projectDeleteID != "" {
		left := "type DELETE to remove " + m.projectDeleteName + ": " + m.projectDeleteInput
		right := styleFooterKey.Render("enter") + " confirm   " + styleFooterKey.Render("esc") + " cancel"
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
		if gap < 1 {
			gap = 1
		}
		return styleFooter.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
	} else if m.worktreeDeleteID != "" {
		left := "type DELETE to remove worktree: " + m.worktreeDeleteInput
		right := styleFooterKey.Render("enter") + " confirm   " + styleFooterKey.Render("esc") + " cancel"
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
		if gap < 1 {
			gap = 1
		}
		return styleFooter.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
	} else if m.projectAdding {
		left := "project path: " + m.projectPath
		right := styleFooterKey.Render("enter") + " add   " + styleFooterKey.Render("esc") + " cancel"
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
		if gap < 1 {
			gap = 1
		}
		return styleFooter.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
	} else if m.worktreeAdding {
		left := "branch: " + m.worktreeBranch
		if m.worktreePathMode {
			left = "path (optional): " + m.worktreePath
		}
		right := styleFooterKey.Render("enter") + " next/create   " + styleFooterKey.Render("esc") + " cancel"
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
		if gap < 1 {
			gap = 1
		}
		return styleFooter.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
	} else if m.scheduleAdding {
		labels := []string{"name: ", "cron: ", "skill: /"}
		values := []string{m.scheduleName, m.scheduleCron, m.scheduleSkill}
		left := labels[m.scheduleField] + values[m.scheduleField]
		right := styleFooterKey.Render("enter") + " next/save   " + styleFooterKey.Render("esc") + " cancel"
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
		if gap < 1 {
			gap = 1
		}
		return styleFooter.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
	} else if m.newSessionCWD != "" {
		left := "new title (optional): " + m.titleInput
		right := styleFooterKey.Render("enter") + " start   " + styleFooterKey.Render("esc") + " cancel"
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
		if gap < 1 {
			gap = 1
		}
		return styleFooter.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
	} else if m.titleEditingID != "" {
		left := "title: " + m.titleInput
		right := styleFooterKey.Render("enter") + " save   " + styleFooterKey.Render("esc") + " cancel"
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
		if gap < 1 {
			gap = 1
		}
		return styleFooter.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
	} else if m.searching {
		left := "search: " + m.searchInput
		right := styleFooterKey.Render("tab") + " next   " + styleFooterKey.Render("enter") + " select   " + styleFooterKey.Render("esc") + " cancel"
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
		if gap < 1 {
			gap = 1
		}
		return styleFooter.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
	} else if m.confirmKillID != "" {
		hints = []string{
			styleFooterKey.Render("y/enter") + " confirm kill",
			styleFooterKey.Render("n/esc") + " cancel",
		}
	} else if m.sessionLocked {
		hints = []string{
			styleFooterKey.Render("ctrl+q") + " back to tree",
			"  typing goes to Claude",
		}
	} else if len(m.projects) == 0 && len(m.schedules) == 0 {
		hints = []string{
			styleFooterKey.Render("a") + " add project",
			styleFooterKey.Render("?") + " help",
			styleFooterKey.Render("q") + " quit",
		}
	} else {
		if schedule := selectedSchedule(m.items, m.cursor); schedule != nil {
			paneHint := styleFooterKey.Render("tab") + " → output"
			if m.rightFocused {
				paneHint = styleFooterKey.Render("tab") + " → tree"
			}
			hints = []string{
				styleFooterKey.Render("r") + " run now",
				styleFooterKey.Render("space") + " enable/disable",
				styleFooterKey.Render("c") + " copy output",
				paneHint,
				styleFooterKey.Render("?") + " help",
				styleFooterKey.Render("q") + " quit",
			}
			left := strings.Join(hints, "   ")
			if m.status != "" {
				right := styleFooterKey.Render(m.status)
				gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
				if gap < 1 {
					gap = 1
				}
				return styleFooter.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
			}
			return styleFooter.Width(m.width).Render(left)
		}
		paneHint := styleFooterKey.Render("tab") + " → output"
		if m.rightFocused {
			paneHint = styleFooterKey.Render("tab") + " → tree"
		}
		hints = []string{
			styleFooterKey.Render("enter") + " expand/attach",
			styleFooterKey.Render("s") + " schedule",
			styleFooterKey.Render("n") + " new session",
			paneHint,
			styleFooterKey.Render("?") + " help",
			styleFooterKey.Render("q") + " quit",
		}
	}
	return styleFooter.Width(m.width).Render(strings.Join(hints, "   "))
}

func (m Model) renderHelp() string {
	bodyH := m.height - headerHeight - footerHeight
	if bodyH < 1 {
		bodyH = 1
	}

	section := lipgloss.NewStyle().Foreground(colorText).Bold(true)
	key := lipgloss.NewStyle().Foreground(colorText).Bold(true)
	muted := styleOutputEmpty.PaddingLeft(0).PaddingTop(0)
	lines := []string{
		"",
		section.Render("Navigation"),
		"",
		key.Render("j / ↓") + "       move down",
		key.Render("k / ↑") + "       move up",
		key.Render("enter") + "       expand or attach",
		key.Render("tab") + "         switch pane",
		key.Render("/") + "            search session titles",
		key.Render("?") + "            show shortcuts",
		"",
		section.Render("Projects and worktrees"),
		"",
		key.Render("a") + "            add project",
		key.Render("w") + "            add worktree",
		key.Render("s") + "            add schedule",
		key.Render("r") + "            run selected schedule / refresh",
		key.Render("space") + "        enable or disable selected schedule",
		key.Render("x") + "            remove selected project or worktree",
		"",
		section.Render("Sessions"),
		"",
		key.Render("n") + "            new session",
		key.Render("e") + "            edit title",
		key.Render("x") + "            remove selected session",
		key.Render("ctrl+q") + "       leave attached session",
		"",
		muted.Render("esc, ?, or q    close shortcuts"),
	}
	content := lipgloss.NewStyle().Width(m.width).Height(bodyH).PaddingLeft(3).Render(strings.Join(lines, "\n"))
	footer := styleFooter.Width(m.width).Render(key.Render("esc") + " close   " + key.Render("q") + " close")
	return lipgloss.JoinVertical(lipgloss.Left,
		styleHeader.Width(m.width).Render("canopy / shortcuts"),
		content,
		footer,
	)
}

func (m Model) renderBody() string {
	bodyH := m.height - headerHeight - footerHeight
	if bodyH < 1 {
		bodyH = 1
	}

	if len(m.projects) == 0 && len(m.schedules) == 0 {
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

	// Find the pinned settings item (always last).
	settingsIdx := -1
	for i, item := range m.items {
		if item.kind == kindSettings {
			settingsIdx = i
			break
		}
	}

	scrollH := treeH
	if settingsIdx >= 0 {
		scrollH = treeH - 1
	}

	var treeContent string
	if m.searchQuery != "" && len(m.items) == 0 {
		treeContent = styleOutputEmpty.Render("No results for \"" + m.searchQuery + "\"")
	} else {
		treeContent = renderTree(m.items, m.cursor, leftW, scrollH)
	}

	left := titleStyle.Width(leftW).Render("WORKSPACE") + "\n" + treeContent
	if settingsIdx >= 0 {
		left += renderTreeItem(treeItem{kind: kindSettings}, m.cursor == settingsIdx, leftW) + "\n"
	}

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
	if m.cursor >= 0 && m.cursor < len(m.items) {
		item := m.items[m.cursor]
		if item.kind == kindSettings {
			lines := []string{
				"",
				lipgloss.NewStyle().Foreground(colorText).Bold(true).PaddingLeft(2).Render("config"),
				"",
				styleOutputEmpty.Render(fmt.Sprintf("%-22s %d", "max_concurrency", m.config.MaxConcurrency)),
				styleOutputEmpty.Render(fmt.Sprintf("%-22s %d", "max_queue_size", m.config.MaxQueueSize)),
				"",
				styleOutputEmpty.Render("stored in canopy.db · settings table"),
			}
			_ = height
			return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
		}
		if item.schedule != nil {
			lastRun := "never"
			if runs := m.runs[item.schedule.ID]; len(runs) > 0 {
				lastRun = runs[0].StartedAt.Local().Format(time.DateTime) + "  " + runs[0].Status
				if runs[0].Output != "" {
					return lipgloss.NewStyle().Width(width).Render("\n" + lipgloss.NewStyle().Foreground(colorText).Bold(true).PaddingLeft(2).Render(item.schedule.Name) + "\n\n" + styleOutputEmpty.Render("cron     "+item.schedule.Cron) + "\n" + styleOutputEmpty.Render("skill    /"+item.schedule.Action) + "\n" + styleOutputEmpty.Render("last run "+lastRun) + "\n\n" + runs[0].Output)
				}
			}
			return lipgloss.NewStyle().Width(width).Render("\n" + lipgloss.NewStyle().Foreground(colorText).Bold(true).PaddingLeft(2).Render(item.schedule.Name) + "\n\n" + styleOutputEmpty.Render("cron     "+item.schedule.Cron) + "\n" + styleOutputEmpty.Render("skill    /"+item.schedule.Action) + "\n" + styleOutputEmpty.Render("last run "+lastRun) + "\n\n" + styleOutputEmpty.Render("r run now   space enable/disable"))
		}
		if item.worktree != nil {
			lines := []string{
				"",
				lipgloss.NewStyle().Foreground(colorText).Bold(true).PaddingLeft(2).Render(item.worktree.Branch),
				"",
				styleOutputEmpty.Render("branch    " + item.worktree.Branch),
				styleOutputEmpty.Render("path      " + item.worktree.Path),
				"",
				styleOutputEmpty.Render("press n to start a session"),
			}
			_ = height
			return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
		}
	}

	sess := selectedSession(m.items, m.cursor)
	if sess == nil {
		return styleOutputEmpty.Width(width).Render(
			"No active session.\nPress n to start a new session.",
		)
	}

	title := sess.Title
	if title == "" {
		title = sess.CWD
	}
	tool := sess.Tool
	if tool == "" {
		tool = "shell"
	}

	lines := []string{
		"",
		lipgloss.NewStyle().Foreground(colorText).Bold(true).PaddingLeft(2).Render(title),
		"",
		lipgloss.NewStyle().PaddingLeft(2).Render(stateDot(sess.State) + "  " + stateLabel(sess.State)),
		"",
		styleOutputEmpty.Render("tool     " + tool),
		styleOutputEmpty.Render("started  " + timeAgo(sess.StartedAt) + " ago"),
		styleOutputEmpty.Render("cwd      " + sess.CWD),
		"",
		styleOutputEmpty.Render("press enter to attach"),
	}
	_ = height
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

// --- helpers ---

func (m *Model) rebuildItems() {
	m.items = buildTree(m.schedules, m.projects, m.worktrees, m.sessions, m.expanded)
	if m.searchQuery != "" {
		m.items = filterTreeItems(m.items, m.searchQuery)
	}
}

func searchResultCursor(items []treeItem, query string) int {
	if query == "" {
		return 0
	}
	q := strings.ToLower(query)
	for i, item := range items {
		if item.kind == kindSession && strings.Contains(strings.ToLower(item.session.Title), q) {
			return i
		}
		if item.kind == kindSchedule && strings.Contains(strings.ToLower(item.schedule.Name), q) {
			return i
		}
	}
	return 0
}

func nextSearchMatch(items []treeItem, cursor int) int {
	for i := cursor + 1; i < len(items); i++ {
		if items[i].kind == kindSession || items[i].kind == kindSchedule {
			return i
		}
	}
	for i := 0; i <= cursor; i++ {
		if items[i].kind == kindSession || items[i].kind == kindSchedule {
			return i
		}
	}
	return cursor
}

func prevSearchMatch(items []treeItem, cursor int) int {
	for i := cursor - 1; i >= 0; i-- {
		if items[i].kind == kindSession || items[i].kind == kindSchedule {
			return i
		}
	}
	for i := len(items) - 1; i >= cursor; i-- {
		if items[i].kind == kindSession || items[i].kind == kindSchedule {
			return i
		}
	}
	return cursor
}

func treeItemKey(items []treeItem, index int) string {
	if index < 0 || index >= len(items) {
		return ""
	}
	item := items[index]
	switch item.kind {
	case kindSession:
		return "session:" + item.session.ID
	case kindSchedule:
		return "schedule:" + item.schedule.ID
	default:
		return ""
	}
}

func findTreeItem(items []treeItem, key string) int {
	if key == "" {
		return 0
	}
	for i := range items {
		if treeItemKey(items, i) == key {
			return i
		}
	}
	return 0
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
	if item.project != nil {
		return item.project.RepoPath
	}
	if item.session != nil {
		return item.session.CWD
	}
	return ""
}

func (m *Model) selectedRepoPath() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	if project := m.items[m.cursor].project; project != nil {
		return project.RepoPath
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

func animationTickCmd() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
		return animationTickMsg{}
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

func updateTitleCmd(sockPath, sessionID, title string) tea.Cmd {
	return func() tea.Msg {
		p, _ := json.Marshal(protocol.UpdateTitleParams{SessionID: sessionID, Title: title})
		if _, err := rpc(sockPath, protocol.Cmd{Type: protocol.CmdUpdateTitle, Payload: p}); err != nil {
			return errMsg("save title: " + err.Error())
		}
		return nil
	}
}

func deleteProjectCmd(dbPath, projectID string) tea.Cmd {
	return func() tea.Msg {
		db, err := store.Open(dbPath)
		if err != nil {
			return errMsg("open store: " + err.Error())
		}
		defer db.Close()
		if err := db.DeleteProject(projectID); err != nil {
			return errMsg("delete project: " + err.Error())
		}
		return fetchDataCmd(dbPath)()
	}
}

func createScheduleCmd(dbPath, name, cron, skill, cwd string) tea.Cmd {
	return func() tea.Msg {
		if _, err := scheduler.ParseCron(cron); err != nil {
			return errMsg("invalid cron: " + err.Error())
		}
		db, err := store.Open(dbPath)
		if err != nil {
			return errMsg("open store: " + err.Error())
		}
		defer db.Close()
		err = db.CreateSchedule(protocol.Schedule{ID: protocol.NewID(), Name: name, ActionType: "skill", Action: strings.TrimPrefix(skill, "/"), Cron: cron, CWD: cwd, Enabled: true})
		if err != nil {
			return errMsg("save schedule: " + err.Error())
		}
		return fetchDataCmd(dbPath)()
	}
}

func toggleScheduleCmd(dbPath string, schedule protocol.Schedule) tea.Cmd {
	return func() tea.Msg {
		db, err := store.Open(dbPath)
		if err != nil {
			return errMsg("open store: " + err.Error())
		}
		defer db.Close()
		if err := db.SetScheduleEnabled(schedule.ID, !schedule.Enabled); err != nil {
			return errMsg("update schedule: " + err.Error())
		}
		return fetchDataCmd(dbPath)()
	}
}

func runScheduleCmd(sockPath, dbPath, scheduleID string) tea.Cmd {
	return func() tea.Msg {
		payload, _ := json.Marshal(protocol.RunScheduleParams{ScheduleID: scheduleID})
		if _, err := rpc(sockPath, protocol.Cmd{Type: protocol.CmdRunSchedule, Payload: payload}); err != nil {
			return errMsg("run schedule: " + err.Error())
		}
		return fetchDataCmd(dbPath)()
	}
}

func copyOutputCmd(output string) tea.Cmd {
	return func() tea.Msg {
		var name string
		switch runtime.GOOS {
		case "darwin":
			name = "pbcopy"
		case "linux":
			for _, candidate := range []string{"wl-copy", "xclip", "xsel"} {
				if _, err := exec.LookPath(candidate); err == nil {
					name = candidate
					break
				}
			}
		default:
			return errMsg("copy output: unsupported platform")
		}
		if name == "" {
			return errMsg("copy output: install wl-clipboard, xclip, or xsel")
		}

		cmd := exec.Command(name)
		if name == "xclip" {
			cmd.Args = []string{name, "-selection", "clipboard"}
		} else if name == "xsel" {
			cmd.Args = []string{name, "--clipboard", "--input"}
		}
		cmd.Stdin = strings.NewReader(output)
		if err := cmd.Run(); err != nil {
			return errMsg("copy output: " + err.Error())
		}
		return copiedMsg{}
	}
}

func clearStatusCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{} })
}

func createSessionCmd(sockPath, cwd, title string, rows, cols uint16) tea.Cmd {
	return func() tea.Msg {
		p, _ := json.Marshal(protocol.NewSessionParams{
			Tool:  "claude",
			CWD:   cwd,
			Title: title,
			Rows:  rows,
			Cols:  cols,
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
