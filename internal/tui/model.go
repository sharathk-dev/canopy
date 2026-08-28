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
	leftPanelRatio = 0.25
	pollInterval   = time.Second
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
	config       protocol.Config
	themeName    string
	themePending string

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

	jumpToSession         bool // set after n key; auto-navigate to first new session
	sessionLocked         bool // when true, keys are forwarded to the active PTY
	lockedSessionID       string
	confirmKillID         string
	titleEditingID        string
	titleInput            string
	newSessionCWD         string
	projectAdding         bool
	picker                picker
	worktreeAdding        bool
	worktreeRepo          string
	worktreeBranch        string
	worktreePath          string
	worktreePathMode      bool
	worktreeField         int
	projectDeleteID       string
	projectDeleteName     string
	projectDeleteInput    string
	worktreeDeleteID      string
	worktreeDeleteRepo    string
	worktreeDeletePath    string
	worktreeDeleteInput   string
	scheduleDeleteID      string
	scheduleDeleteName    string
	scheduleDeleteInput   string
	scheduleAdding        bool
	scheduleField         int
	scheduleName          string
	scheduleProject       string
	scheduleProjectCursor int
	projectPickerOpen     bool
	scheduleCron          string
	scheduleSkill         string
	skillOptions          []string
	skillCursor           int
	searching             bool
	searchInput           string
	searchQuery           string
	searchPrevious        string
	cronError             string
	showHelp              bool
	daemonDown            bool
	status                string
	sessionsSized         bool
	err                   string
}

// inputMode identifies which part of the TUI owns keyboard input. Modes are
// ordered from most specific to least specific; a modal or attached session
// must consume input before tree navigation or global shortcuts can see it.
type inputMode uint8

const (
	modeNavigation inputMode = iota
	modeModal
	modeSearch
	modeHelp
	modeAttached
)

func (m Model) inputMode() inputMode {
	switch {
	case m.sessionLocked:
		return modeAttached
	case m.showHelp:
		return modeHelp
	case m.searching:
		return modeSearch
	case m.projectDeleteID != "", m.worktreeDeleteID != "", m.scheduleDeleteID != "":
		return modeModal
	case m.scheduleAdding, m.projectAdding, m.worktreeAdding,
		m.newSessionCWD != "", m.titleEditingID != "", m.confirmKillID != "":
		return modeModal
	default:
		return modeNavigation
	}
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
				cmds = append(cmds, resizeAndRedrawSessionCmd(m.sockPath, sess.ID, rows, cols))
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
		if m.themePending != "" {
			if msg.themeName == m.themePending {
				m.themePending = ""
			} else {
				msg.themeName = m.themePending
			}
		}
		if msg.themeName != m.themeName {
			m.themeName = msg.themeName
			SetTheme(ThemeByName(m.themeName))
		}
		if m.ready && !m.sessionsSized {
			rows, cols := m.panelSize()
			for _, wtSessions := range m.sessions {
				for _, sess := range wtSessions {
					cmds = append(cmds, resizeAndRedrawSessionCmd(m.sockPath, sess.ID, rows, cols))
				}
			}
			m.sessionsSized = true
		}
		for _, p := range m.projects {
			if _, seen := m.expanded["p:"+p.ID]; !seen {
				m.expanded["p:"+p.ID] = true
			}
			for _, wt := range m.worktrees[p.RepoPath] {
				if _, seen := m.expanded["w:"+wt.ID]; !seen {
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
		return m.handleKey(msg, cmds)
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	// Route the event to exactly one owner. This prevents text-input modes
	// from accidentally falling through to navigation/global shortcuts.
	switch m.inputMode() {
	case modeAttached:
		return m.handleLockedKey(msg, cmds)
	case modeHelp:
		return m.handleHelpKey(msg, cmds)
	case modeSearch:
		return m.handleSearchKey(msg, cmds)
	case modeModal:
		return m.handleModalKey(msg, cmds)
	default:
		return m.handleNavigationKey(msg, cmds)
	}
}

func (m Model) handleHelpKey(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "?", "q":
		m.showHelp = false
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleSearchKey(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
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
	case "tab":
		m.cursor = nextSearchMatch(m.items, m.cursor)
	case "shift+tab":
		m.cursor = prevSearchMatch(m.items, m.cursor)
	case "down":
		m.cursor = nextSearchMatch(m.items, m.cursor)
	case "up":
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

// handleModalKey owns all form and confirmation interactions. The existing
// modal implementations remain together for now so their behavior stays
// unchanged while routing is made explicit.
func (m Model) handleModalKey(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	// Modal input owns the keyboard. In particular, do not let the global quit
	// shortcut escape a confirmation or text-entry flow unexpectedly.
	if msg.String() == "ctrl+c" {
		return m, tea.Batch(cmds...)
	}
	return m.handleNavigationKey(msg, cmds)
}

func (m Model) handleNavigationKey(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
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

	if m.scheduleDeleteID != "" {
		switch msg.String() {
		case "enter":
			if m.scheduleDeleteInput == "DELETE" {
				id := m.scheduleDeleteID
				m.scheduleDeleteID = ""
				m.scheduleDeleteName = ""
				m.scheduleDeleteInput = ""
				cmds = append(cmds, archiveScheduleCmd(m.dbPath, id))
			}
		case "esc":
			m.scheduleDeleteID = ""
			m.scheduleDeleteName = ""
			m.scheduleDeleteInput = ""
		case "backspace", "ctrl+h":
			if len(m.scheduleDeleteInput) > 0 {
				runes := []rune(m.scheduleDeleteInput)
				m.scheduleDeleteInput = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) > 0 {
				m.scheduleDeleteInput += string(msg.Runes)
			}
		}
		return m, tea.Batch(cmds...)
	}

	if m.scheduleAdding {
		fields := []*string{&m.scheduleName, &m.scheduleProject, &m.scheduleCron, &m.scheduleSkill}
		switch msg.String() {
		case "tab":
			m.scheduleField = (m.scheduleField + 1) % len(fields)
		case "shift+tab":
			m.scheduleField = (m.scheduleField + len(fields) - 1) % len(fields)
		case "up", "down":
			if m.scheduleField == 1 {
				m.projectPickerOpen = true
				optionCount := len(m.projects) + 1
				if optionCount > 0 {
					if msg.String() == "up" {
						m.scheduleProjectCursor = (m.scheduleProjectCursor + optionCount - 1) % optionCount
					} else {
						m.scheduleProjectCursor = (m.scheduleProjectCursor + 1) % optionCount
					}
				}
			} else if m.scheduleField == 3 {
				options := m.filteredSkillOptions()
				if len(options) > 0 {
					if msg.String() == "up" {
						m.skillCursor = (m.skillCursor + len(options) - 1) % len(options)
					} else {
						m.skillCursor = (m.skillCursor + 1) % len(options)
					}
				}
			}
		case "enter":
			if m.scheduleField == 1 {
				if !m.projectPickerOpen {
					m.projectPickerOpen = true
					return m, tea.Batch(cmds...)
				}
				if m.scheduleProjectCursor > 0 {
					m.scheduleProject = m.projects[m.scheduleProjectCursor-1].RepoPath
				} else {
					m.scheduleProject = ""
				}
				m.skillOptions = discoverSkills(m.scheduleProject)
				m.projectPickerOpen = false
				m.scheduleField = 2
				return m, tea.Batch(cmds...)
			}
			if m.scheduleField == 3 {
				options := m.filteredSkillOptions()
				if len(options) > 0 && strings.TrimPrefix(m.scheduleSkill, "/") != options[m.skillCursor] {
					m.scheduleSkill = options[m.skillCursor]
					return m, tea.Batch(cmds...)
				}
			}
			if strings.TrimSpace(*fields[m.scheduleField]) == "" {
				return m, tea.Batch(cmds...)
			}
			// If all fields are filled, submit; otherwise move to next empty field.
			allFilled := strings.TrimSpace(m.scheduleName) != "" &&
				strings.TrimSpace(m.scheduleCron) != "" &&
				strings.TrimSpace(m.scheduleSkill) != ""
			if allFilled {
				name, cron, skill := strings.TrimSpace(m.scheduleName), strings.TrimSpace(m.scheduleCron), strings.TrimSpace(m.scheduleSkill)
				cwd := m.scheduleProject
				m.scheduleAdding = false
				m.scheduleField = 0
				m.scheduleName, m.scheduleCron, m.scheduleSkill = "", "", ""
				m.scheduleProject = ""
				m.scheduleProjectCursor = 0
				m.skillOptions = nil
				m.skillCursor = 0
				m.projectPickerOpen = false
				return m, tea.Batch(createScheduleCmd(m.dbPath, name, cron, skill, cwd))
			}
			if m.scheduleField < len(fields)-1 {
				m.scheduleField++
			}
		case "esc":
			if m.projectPickerOpen {
				m.projectPickerOpen = false
				return m, tea.Batch(cmds...)
			}
			m.scheduleAdding = false
			m.scheduleField = 0
			m.scheduleName, m.scheduleCron, m.scheduleSkill = "", "", ""
			m.scheduleProject = ""
			m.scheduleProjectCursor = 0
			m.skillOptions = nil
			m.skillCursor = 0
			m.projectPickerOpen = false
		case "backspace", "ctrl+h":
			if m.scheduleField != 1 {
				field := fields[m.scheduleField]
				if len(*field) > 0 {
					runes := []rune(*field)
					*field = string(runes[:len(runes)-1])
				}
				if m.scheduleField == 3 {
					m.skillCursor = 0
				}
			}
		default:
			if len(msg.Runes) > 0 && m.scheduleField != 1 {
				*fields[m.scheduleField] += string(msg.Runes)
				if m.scheduleField == 3 {
					m.skillCursor = 0
				}
			}
		}
		if strings.TrimSpace(m.scheduleCron) != "" {
			if _, err := scheduler.ParseCron(strings.TrimSpace(m.scheduleCron)); err != nil {
				m.cronError = err.Error()
			} else {
				m.cronError = ""
			}
		} else {
			m.cronError = ""
		}
		return m, tea.Batch(cmds...)
	}

	if m.projectAdding {
		switch msg.String() {
		case "enter":
			if len(m.picker.matches) > 0 && m.picker.cursor < len(m.picker.matches) {
				path := m.picker.matches[m.picker.cursor].path
				m.projectAdding = false
				return m, tea.ExecProcess(
					exec.Command(executablePath(), "project", "add", path),
					func(error) tea.Msg { return fetchDataCmd(m.dbPath)() },
				)
			}
		case "esc":
			m.projectAdding = false
		case "up", "k":
			if m.picker.cursor > 0 {
				m.picker.cursor--
				m.picker.clampCursor()
			}
		case "down", "j":
			if m.picker.cursor < len(m.picker.matches)-1 {
				m.picker.cursor++
				m.picker.clampCursor()
			}
		case "backspace", "ctrl+h":
			if len(m.picker.input) > 0 {
				runes := []rune(m.picker.input)
				m.picker.input = string(runes[:len(runes)-1])
				m.picker.filter()
				m.picker.cursor = 0
				m.picker.offset = 0
			}
		default:
			if len(msg.Runes) > 0 {
				m.picker.input += string(msg.Runes)
				m.picker.filter()
				m.picker.cursor = 0
				m.picker.offset = 0
			}
		}
		return m, tea.Batch(cmds...)
	}

	if m.worktreeAdding {
		switch msg.String() {
		case "tab", "shift+tab":
			if msg.String() == "tab" {
				m.worktreeField = (m.worktreeField + 1) % 2
			} else {
				m.worktreeField = (m.worktreeField + 1) % 2
			}
		case "enter":
			if m.worktreeField == 0 && strings.TrimSpace(m.worktreeBranch) != "" {
				m.worktreeField = 1
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
			m.worktreeField = 0
			m.worktreeField = 0
			m.worktreeField = 0
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
			m.worktreeField = 0
		case "backspace", "ctrl+h":
			field := &m.worktreeBranch
			if m.worktreeField == 1 {
				field = &m.worktreePath
			}
			if len(*field) > 0 {
				runes := []rune(*field)
				*field = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) > 0 {
				if m.worktreeField == 1 {
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
		m.picker = newPicker(pickerHeight)

	case "w":
		if repo := m.selectedRepoPath(); repo != "" {
			m.worktreeAdding = true
			m.worktreeRepo = repo
			m.worktreeBranch = ""
			m.worktreePath = ""
			m.worktreePathMode = false
			m.worktreeField = 0
		}

	case "t":
		if m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor].kind == kindSettings {
			next := NextThemeName(m.themeName)
			m.themeName = next
			m.themePending = next
			SetTheme(ThemeByName(next))
			cmds = append(cmds, saveThemeCmd(m.dbPath, next))
		}

	case "s":
		m.scheduleAdding = true
		m.scheduleField = 0
		m.scheduleName, m.scheduleCron, m.scheduleSkill, m.scheduleProject = "", "", "", ""
		m.scheduleProjectCursor = 0
		if m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor].project != nil {
			m.scheduleProject = m.items[m.cursor].project.RepoPath
			for i := range m.projects {
				if m.projects[i].RepoPath == m.scheduleProject {
					m.scheduleProjectCursor = i + 1
					break
				}
			}
		}
		m.cronError = ""
		m.skillOptions = discoverSkills(m.scheduleProject)
		m.skillCursor = 0

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
		if schedule := selectedSchedule(m.items, m.cursor); schedule != nil {
			m.scheduleDeleteID = schedule.ID
			m.scheduleDeleteName = schedule.Name
			m.scheduleDeleteInput = ""
		} else if m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor].kind == kindProject {
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
		// Resize must complete before taking the snapshot. Running these as a
		// batch races the fetch against the PTY resize and can leave Claude's
		// status line rendered for the previous pane width.
		*cmds = append(*cmds, resizeAndFetchSnapshotCmd(m.sockPath, sess.ID, rows, cols))
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

	view := lipgloss.JoinVertical(lipgloss.Left,
		m.renderBody(),
		m.renderFooter(),
	)
	if colorPanel != "" {
		view = lipgloss.NewStyle().Background(colorPanel).Width(m.width).Height(m.height).Render(view)
	}
	if m.projectAdding {
		view = m.renderPickerModal()
	}
	if m.scheduleAdding {
		view = m.renderScheduleModal(view)
	}
	if m.titleEditingID != "" {
		view = m.renderTitleModal()
	}
	if m.projectDeleteID != "" || m.worktreeDeleteID != "" {
		view = m.renderDeleteModal()
	}
	if m.worktreeAdding {
		view = m.renderWorktreeModal()
	}
	if m.scheduleDeleteID != "" {
		view = m.renderDeleteModal()
	}
	if m.newSessionCWD != "" {
		view = m.renderNewSessionModal()
	}
	if m.confirmKillID != "" {
		view = m.renderKillModal()
	}
	return restorePanelBackground(view)
}

func (m Model) renderScheduleModal(background string) string {
	const modalWidth = 76

	dimStyle := themed(colorDim)
	textStyle := themed(colorText)
	boldStyle := themed(colorText).Bold(true)
	titleStyle := themed(colorFocus).Bold(true)

	innerW := modalWidth - 2 // subtract border

	fieldLabels := []string{"Name", "Project (optional)", "Cron", "Skill"}
	fieldValues := []string{m.scheduleName, m.scheduleProjectName(), m.scheduleCron, m.scheduleSkill}
	fieldHints := []string{"schedule name", "select a project", "0 9 * * 1-5", "skill-name"}
	fieldPrefixes := []string{"", "", "", "/"}

	var lines []string
	lines = append(lines, titleStyle.Render("Add Schedule"), "")

	for i, label := range fieldLabels {
		active := i == m.scheduleField
		prefix := fieldPrefixes[i]
		value := fieldValues[i]
		hint := fieldHints[i]

		var labelRendered string
		labelText := fmt.Sprintf("%-18s:", label)
		if active {
			labelRendered = boldStyle.Render(labelText)
		} else {
			labelRendered = dimStyle.Render(labelText)
		}

		var valueRendered string
		if value == "" {
			placeholder := dimStyle.Render(prefix + hint)
			if active {
				valueRendered = "█" + placeholder
			} else {
				valueRendered = placeholder
			}
		} else if active {
			valueRendered = textStyle.Render(prefix+value) + "█"
		} else {
			valueRendered = dimStyle.Render(prefix + value)
		}
		if i == 2 && m.cronError != "" {
			valueRendered += "  " + themed(colorTerminated).Render(m.cronError)
		}

		fieldLine := "  " + labelRendered + " " + valueRendered
		lines = append(lines, lipgloss.NewStyle().Width(innerW).Render(fieldLine))
		if i == 1 && active && m.projectPickerOpen {
			options := append([]string{"Global only"}, projectNames(m.projects)...)
			for j, option := range options {
				if j >= 5 {
					break
				}
				line := "  " + option
				if j == m.scheduleProjectCursor {
					lines = append(lines, styleTreeSelected.Width(innerW).Render(line))
				} else {
					lines = append(lines, dimStyle.Render(line))
				}
			}
		} else if i == 3 && active {
			options := m.filteredSkillOptions()
			if len(options) > 0 {
				selected := m.skillCursor
				if selected >= len(options) {
					selected = 0
				}
				for j, option := range options {
					if j >= 5 {
						break
					}
					line := "  /" + option
					if j == selected {
						lines = append(lines, styleTreeSelected.Width(innerW).Render(line))
					} else {
						lines = append(lines, dimStyle.Render(line))
					}
				}
			}
		}
		lines = append(lines, "") // spacing between fields
	}

	// Bottom hint line
	enterStyle := styleFooterKey
	if !m.scheduleFormReady() {
		enterStyle = dimStyle
	}
	hintLine := styleFooterKey.Render("tab") + dimStyle.Render(" next   ") +
		enterStyle.Render("enter") + dimStyle.Render(" confirm   ") +
		styleFooterKey.Render("esc") + dimStyle.Render(" cancel")
	lines = append(lines, themed(colorText).Width(innerW).Render("  "+hintLine))

	content := strings.Join(lines, "\n")

	modal := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorFocus).
		Background(colorPanel).
		Width(innerW).
		Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceBackground(lipgloss.NoColor{}),
	)
}

func (m Model) scheduleProjectName() string {
	for _, project := range m.projects {
		if project.RepoPath == m.scheduleProject {
			return project.Name
		}
	}
	return ""
}

func projectNames(projects []protocol.Project) []string {
	names := make([]string, 0, len(projects))
	for _, project := range projects {
		names = append(names, project.Name)
	}
	return names
}

func (m Model) scheduleFormReady() bool {
	if strings.TrimSpace(m.scheduleName) == "" || strings.TrimSpace(m.scheduleCron) == "" || strings.TrimSpace(m.scheduleSkill) == "" {
		return false
	}
	_, err := scheduler.ParseCron(strings.TrimSpace(m.scheduleCron))
	return err == nil
}

func (m Model) renderFooter() string {
	var hints []string
	if m.searching {
		hints = []string{
			styleFooterKey.Render("↑↓") + " navigate",
			styleFooterKey.Render("enter") + " select",
			styleFooterKey.Render("esc") + " cancel",
		}
		return restoreBaseStyle(styleFooter.Width(m.width).Render(strings.Join(hints, "   ")))
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
	} else if m.rightFocused {
		hints = []string{
			styleFooterKey.Render("tab") + " → tree",
			styleFooterKey.Render("c") + " copy",
			styleFooterKey.Render("?") + " help",
			styleFooterKey.Render("q") + " quit",
		}
	} else if m.cursor >= 0 && m.cursor < len(m.items) {
		item := m.items[m.cursor]
		switch item.kind {
		case kindProject:
			hints = []string{
				styleFooterKey.Render("enter") + " expand",
				styleFooterKey.Render("n") + " new session",
				styleFooterKey.Render("w") + " add worktree",
				styleFooterKey.Render("x") + " remove",
				styleFooterKey.Render("/") + " search",
				styleFooterKey.Render("?") + " help",
				styleFooterKey.Render("q") + " quit",
			}
		case kindWorktree:
			hints = []string{
				styleFooterKey.Render("enter") + " expand",
				styleFooterKey.Render("n") + " new session",
				styleFooterKey.Render("w") + " add worktree",
				styleFooterKey.Render("x") + " remove",
				styleFooterKey.Render("/") + " search",
				styleFooterKey.Render("?") + " help",
				styleFooterKey.Render("q") + " quit",
			}
		case kindSession:
			hints = []string{
				styleFooterKey.Render("enter") + " attach",
				styleFooterKey.Render("e") + " rename",
				styleFooterKey.Render("x") + " remove",
				styleFooterKey.Render("/") + " search",
				styleFooterKey.Render("tab") + " → output",
				styleFooterKey.Render("?") + " help",
				styleFooterKey.Render("q") + " quit",
			}
		case kindSchedule:
			hints = []string{
				styleFooterKey.Render("r") + " run",
				styleFooterKey.Render("space") + " enable/disable",
				styleFooterKey.Render("x") + " remove",
				styleFooterKey.Render("c") + " copy output",
				styleFooterKey.Render("tab") + " → output",
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
				return restoreBaseStyle(styleFooter.Width(m.width).Render(left + strings.Repeat(" ", gap) + right))
			}
			return restoreBaseStyle(styleFooter.Width(m.width).Render(left))
		default: // kindSettings
			hints = []string{
				styleFooterKey.Render("t") + " toggle theme",
				styleFooterKey.Render("?") + " help",
				styleFooterKey.Render("q") + " quit",
			}
		}
	}
	return restoreBaseStyle(styleFooter.Width(m.width).Render(strings.Join(hints, "   ")))
}

func (m Model) renderHelp() string {
	bodyH := m.height - footerHeight
	if bodyH < 1 {
		bodyH = 1
	}

	section := themed(colorText).Bold(true)
	key := themed(colorText).Bold(true)
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
		key.Render("x") + "            remove selected schedule",
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
	bodyH := m.height - footerHeight
	if bodyH < 1 {
		bodyH = 1
	}

	if len(m.projects) == 0 && len(m.schedules) == 0 {
		return m.renderEmptyState(bodyH)
	}

	rightActive := m.rightFocused || m.sessionLocked

	lc := lipgloss.Color(colorFocus)
	rc := lipgloss.Color(colorBorder)
	if rightActive {
		lc = colorBorder
		rc = colorFocus
	}

	// Each panel has a full lipgloss border (2 chars wide, 2 rows tall).
	// Two panels side by side: (leftInnerW+2) + (rightInnerW+2) = m.width
	innerH := bodyH - 2
	if innerH < 1 {
		innerH = 1
	}
	usableW := m.width - 4
	leftInnerW := int(float64(usableW) * leftPanelRatio)
	if leftInnerW < 20 {
		leftInnerW = 20
	}
	rightInnerW := usableW - leftInnerW
	if rightInnerW < 10 {
		rightInnerW = 10
	}

	// Left panel content.
	titleStyle := stylePanelTitle
	if !rightActive {
		titleStyle = stylePanelTitle.Foreground(colorFocus)
	}

	settingsIdx := -1
	for i, item := range m.items {
		if item.kind == kindSettings {
			settingsIdx = i
			break
		}
	}

	scrollH := innerH - 1 // -1 for WORKSPACE title
	if settingsIdx >= 0 {
		scrollH = innerH - 2 // -1 for pinned settings too
	}
	if scrollH < 1 {
		scrollH = 1
	}

	var treeContent string
	if m.searchQuery != "" && len(m.items) == 0 {
		treeContent = styleOutputEmpty.Render("No results for \"" + m.searchQuery + "\"")
	} else {
		treeContent = renderTree(m.items, m.cursor, leftInnerW, scrollH)
	}

	leftHeader := titleStyle.Render("WORKSPACE")
	if m.searching {
		searchValue := m.searchInput
		if searchValue == "" {
			leftHeader = themed(colorDim).PaddingLeft(1).Render("/█type to search")
		} else {
			leftHeader = themed(colorText).PaddingLeft(1).Render("/" + searchValue + "█")
		}
	}
	leftContent := themed(colorText).Width(leftInnerW).Render(leftHeader) + "\n" + treeContent
	if settingsIdx >= 0 {
		leftContent += renderTreeItem(treeItem{kind: kindSettings}, m.cursor == settingsIdx, leftInnerW)
	}

	// Right panel content.
	var rightContent string
	if m.output == "" {
		rightContent = m.renderDetail(rightInnerW, innerH)
	} else {
		rightContent = m.viewport.View()
	}

	// Wrap each panel in a lipgloss border. Width/Height set the inner content
	// dimensions; the border chars are added outside. This avoids per-line ANSI
	// measurement which breaks on raw PTY output.
	leftPanel := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lc).
		Background(colorPanel).
		Width(leftInnerW).
		Height(innerH).
		Render(leftContent)

	rightPanel := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(rc).
		Background(colorPanel).
		Width(rightInnerW).
		Height(innerH).
		Render(rightContent)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
}

func (m Model) renderEmptyState(bodyH int) string {
	pad := strings.Repeat("\n", bodyH/3)
	content := pad +
		themed(colorText).Bold(true).PaddingLeft(4).Render("No projects yet.") + "\n\n" +
		styleOutputEmpty.Render("Press  a  to add the current directory as a project.") + "\n" +
		styleOutputEmpty.Render("Or run: canopy project add")
	return lipgloss.NewStyle().Width(m.width).Height(bodyH).Render(content)
}

func (m Model) renderDetail(width, height int) string {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		item := m.items[m.cursor]
		if item.kind == kindSettings {
			themeName := m.themeName
			if themeName == "" {
				themeName = "system"
			}
			activeStyle := themed(colorKey).Bold(true)
			dimStyle := themed(colorDim)
			var themeParts []string
			for _, n := range ThemeNames {
				if n == themeName {
					themeParts = append(themeParts, activeStyle.Render("["+n+"]"))
				} else {
					themeParts = append(themeParts, dimStyle.Render(n))
				}
			}
			themeRow := fmt.Sprintf("%-22s %s", "theme", strings.Join(themeParts, "  "))
			lines := []string{
				"",
				themed(colorText).Bold(true).PaddingLeft(2).Render("config"),
				"",
				styleOutputEmpty.Render(fmt.Sprintf("%-22s %d", "max_scheduler_concurrency", m.config.MaxSchedulerConcurrency)),
				styleOutputEmpty.Render(fmt.Sprintf("%-22s %d", "max_scheduler_queue_size", m.config.MaxSchedulerQueueSize)),
				styleOutputEmpty.Render(themeRow),
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
					return themed(colorText).Width(width).Render("\n" + themed(colorText).Bold(true).PaddingLeft(2).Render(item.schedule.Name) + "\n\n" + styleOutputEmpty.Render("cron     "+item.schedule.Cron) + "\n" + styleOutputEmpty.Render("skill    /"+item.schedule.Action) + "\n" + styleOutputEmpty.Render("last run "+lastRun) + "\n\n" + runs[0].Output)
				}
			}
			return themed(colorText).Width(width).Render("\n" + themed(colorText).Bold(true).PaddingLeft(2).Render(item.schedule.Name) + "\n\n" + styleOutputEmpty.Render("cron     "+item.schedule.Cron) + "\n" + styleOutputEmpty.Render("skill    /"+item.schedule.Action) + "\n" + styleOutputEmpty.Render("last run "+lastRun) + "\n\n" + styleOutputEmpty.Render("r run now   space enable/disable"))
		}
		if item.worktree != nil {
			lines := []string{
				"",
				themed(colorText).Bold(true).PaddingLeft(2).Render(item.worktree.Branch),
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
		themed(colorText).Bold(true).PaddingLeft(2).Render(title),
		"",
		themed(colorText).PaddingLeft(2).Render(stateDot(sess.State) + "  " + stateLabel(sess.State)),
		"",
		styleOutputEmpty.Render("tool     " + tool),
		styleOutputEmpty.Render("started  " + timeAgo(sess.StartedAt) + " ago"),
		styleOutputEmpty.Render("cwd      " + sess.CWD),
		"",
		styleOutputEmpty.Render("press enter to attach"),
	}
	_ = height
	return themed(colorText).Width(width).Render(strings.Join(lines, "\n"))
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
	bodyH := m.height - footerHeight
	innerH := bodyH - 2
	usableW := m.width - 4
	leftInnerW := int(float64(usableW) * leftPanelRatio)
	if leftInnerW < 20 {
		leftInnerW = 20
	}
	rightInnerW := usableW - leftInnerW
	if rightInnerW < 10 {
		rightInnerW = 10
	}
	if innerH < 1 {
		innerH = 1
	}
	m.viewport = viewport.New(rightInnerW, innerH)
	m.viewport.SetContent(m.output)
}

// panelSize returns the (rows, cols) of the right panel in terminal cells.
func (m *Model) panelSize() (uint16, uint16) {
	bodyH := m.height - footerHeight
	innerH := bodyH - 2
	usableW := m.width - 4
	leftInnerW := int(float64(usableW) * leftPanelRatio)
	if leftInnerW < 20 {
		leftInnerW = 20
	}
	rightInnerW := usableW - leftInnerW
	if rightInnerW < 10 {
		rightInnerW = 10
	}
	if innerH < 1 {
		innerH = 1
	}
	return uint16(innerH), uint16(rightInnerW)
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

func resizeAndFetchSnapshotCmd(sockPath, sessionID string, rows, cols uint16) tea.Cmd {
	return func() tea.Msg {
		p, _ := json.Marshal(protocol.ResizeSessionParams{SessionID: sessionID, Rows: rows, Cols: cols})
		_, _ = rpc(sockPath, protocol.Cmd{Type: protocol.CmdResizeSession, Payload: p})
		// Claude redraws asynchronously after SIGWINCH. Give it a moment to
		// update its status bar before reading the virtual terminal snapshot.
		time.Sleep(100 * time.Millisecond)
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

func resizeAndRedrawSessionCmd(sockPath, sessionID string, rows, cols uint16) tea.Cmd {
	return func() tea.Msg {
		p, _ := json.Marshal(protocol.ResizeSessionParams{SessionID: sessionID, Rows: rows, Cols: cols})
		_, _ = rpc(sockPath, protocol.Cmd{Type: protocol.CmdResizeSession, Payload: p})

		// Claude redraws its status bar when it receives Ctrl+L. This is the
		// same redraw users can trigger manually, but it is safe to do once
		// after a layout resize instead of leaving stale truncated content.
		input, _ := json.Marshal(protocol.InputParams{SessionID: sessionID, Data: []byte{0x0c}})
		_, _ = rpc(sockPath, protocol.Cmd{Type: protocol.CmdInput, Payload: input})
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

func saveThemeCmd(dbPath, name string) tea.Cmd {
	return func() tea.Msg {
		db, err := store.Open(dbPath)
		if err != nil {
			return errMsg(err.Error())
		}
		defer db.Close()
		_ = db.SetSetting("theme", name)
		return nil
	}
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

func killSessionCmd(sockPath, dbPath, sessionID string, _ int) tea.Cmd {
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

func archiveScheduleCmd(dbPath, scheduleID string) tea.Cmd {
	return func() tea.Msg {
		db, err := store.Open(dbPath)
		if err != nil {
			return errMsg(err.Error())
		}
		defer db.Close()
		if err := db.ArchiveSchedule(scheduleID); err != nil {
			return errMsg("archive schedule: " + err.Error())
		}
		return fetchDataCmd(dbPath)()
	}
}
