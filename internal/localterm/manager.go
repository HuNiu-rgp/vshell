package localterm

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

const flushInterval = 50 * time.Millisecond

type Session struct {
	id      string
	cmd     *exec.Cmd
	ptyFile *os.File
	onEvent func(string, any)
	once    sync.Once
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	onEvent  func(string, any)
}

func NewManager(onEvent func(string, any)) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		onEvent:  onEvent,
	}
}

func (m *Manager) Start(sessionID string, rows, cols uint16) error {
	return m.StartInDir(sessionID, "", rows, cols)
}

func (m *Manager) StartInDir(sessionID string, dir string, rows, cols uint16) error {
	cmd := localShellCommand()
	if dir != "" {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("resolve local shell directory: %w", err)
		}
		info, err := os.Stat(absDir)
		if err != nil {
			return fmt.Errorf("stat local shell directory: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("local shell path is not a directory: %s", absDir)
		}
		cmd.Dir = absDir
	} else if home, err := os.UserHomeDir(); err == nil && home != "" {
		cmd.Dir = home
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return fmt.Errorf("start local shell: %w", err)
	}

	session := &Session{
		id:      sessionID,
		cmd:     cmd,
		ptyFile: f,
		onEvent: m.onEvent,
	}

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	writer := newFlushingWriter(sessionID, "terminal:stdout", m.onEvent)
	go func() {
		io.Copy(writer, f)
		writer.Flush()
	}()

	go func() {
		cmd.Wait()
		session.Close()
		m.mu.Lock()
		delete(m.sessions, sessionID)
		m.mu.Unlock()
		m.onEvent("terminal:closed", map[string]any{
			"sessionID": sessionID,
		})
	}()

	return nil
}

func (m *Manager) Cwd(sessionID string) (string, error) {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("local terminal session not found")
	}
	if session.cmd == nil || session.cmd.Process == nil {
		return "", fmt.Errorf("local terminal process not available")
	}
	return processCwd(session.cmd.Process.Pid)
}

func (m *Manager) WriteStdin(sessionID string, data []byte) bool {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	_, _ = session.ptyFile.Write(data)
	return true
}

func (m *Manager) Resize(sessionID string, rows, cols uint16) bool {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	_ = pty.Setsize(session.ptyFile, &pty.Winsize{Rows: rows, Cols: cols})
	return true
}

func (m *Manager) DisconnectSession(sessionID string) bool {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	session.Close()
	return true
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()
	for _, session := range sessions {
		session.Close()
	}
}

func (s *Session) Close() {
	s.once.Do(func() {
		_ = s.ptyFile.Close()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	})
}

func localShellCommand() *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd.exe")
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		if runtime.GOOS == "darwin" {
			shell = "/bin/zsh"
		} else {
			shell = "/bin/bash"
		}
	}
	return exec.Command(shell, "-l")
}

func processCwd(pid int) (string, error) {
	switch runtime.GOOS {
	case "linux":
		return os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	case "darwin":
		out, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
		if err != nil {
			return "", fmt.Errorf("read process cwd: %w", err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "n") && len(line) > 1 {
				return line[1:], nil
			}
		}
		return "", fmt.Errorf("process cwd not found")
	default:
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home, nil
		}
		return "", fmt.Errorf("current directory lookup is not supported on %s", runtime.GOOS)
	}
}

type flushingWriter struct {
	sessionID   string
	eventName   string
	onEvent     func(string, any)
	mu          sync.Mutex
	pending     []byte
	flushTicker *time.Ticker
	done        chan struct{}
}

func newFlushingWriter(sessionID, eventName string, onEvent func(string, any)) *flushingWriter {
	fw := &flushingWriter{
		sessionID:   sessionID,
		eventName:   eventName,
		onEvent:     onEvent,
		flushTicker: time.NewTicker(flushInterval),
		done:        make(chan struct{}),
	}
	go fw.tickFlush()
	return fw
}

func (fw *flushingWriter) Write(p []byte) (int, error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.pending = append(fw.pending, p...)
	return len(p), nil
}

func (fw *flushingWriter) Flush() {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.doFlush()
}

func (fw *flushingWriter) doFlush() {
	if len(fw.pending) == 0 {
		return
	}
	data := make([]byte, len(fw.pending))
	copy(data, fw.pending)
	fw.pending = fw.pending[:0]
	fw.onEvent(fw.eventName, map[string]any{
		"sessionID": fw.sessionID,
		"data":      string(data),
	})
}

func (fw *flushingWriter) tickFlush() {
	for {
		select {
		case <-fw.flushTicker.C:
			fw.mu.Lock()
			fw.doFlush()
			fw.mu.Unlock()
		case <-fw.done:
			fw.flushTicker.Stop()
			return
		}
	}
}
