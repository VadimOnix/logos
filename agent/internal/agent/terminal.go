package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// F10: remote terminal. The server opens an interactive shell over the
// management channel (term_open / term_data / term_close). The agent
// allocates a pty and runs the system shell in it; every session is
// initiated and audited by the control plane, and everything dies with the
// channel — a dropped connection cannot leave shells running unattended.

const (
	maxTerminals    = 2
	termIdleTimeout = 30 * time.Minute
)

// termSession is one live pty + shell.
type termSession struct {
	id     string
	ptmx   *os.File
	cmd    *exec.Cmd
	cancel context.CancelFunc

	activityMu sync.Mutex
	lastActive time.Time
}

func (t *termSession) touch() {
	t.activityMu.Lock()
	t.lastActive = time.Now()
	t.activityMu.Unlock()
}

func (t *termSession) idleSince() time.Time {
	t.activityMu.Lock()
	defer t.activityMu.Unlock()
	return t.lastActive
}

// termManager tracks the sessions of one channel connection.
type termManager struct {
	mu       sync.Mutex
	sessions map[string]*termSession
	log      *slog.Logger
	send     func(wireMsg) // serialized channel writer
}

func newTermManager(log *slog.Logger, send func(wireMsg)) *termManager {
	return &termManager{sessions: map[string]*termSession{}, log: log, send: send}
}

// openPTY allocates a pty pair via /dev/ptmx (Linux; OpenWrt included).
func openPTY() (ptmx, tty *os.File, err error) {
	ptmx, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}
	defer func() {
		if err != nil {
			ptmx.Close()
		}
	}()
	n, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPTN)
	if err != nil {
		return nil, nil, fmt.Errorf("TIOCGPTN: %w", err)
	}
	// Unlock the slave side.
	unlock := 0
	if err = unix.IoctlSetPointerInt(int(ptmx.Fd()), unix.TIOCSPTLCK, unlock); err != nil {
		return nil, nil, fmt.Errorf("TIOCSPTLCK: %w", err)
	}
	tty, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	return ptmx, tty, nil
}

func shellPath() string {
	for _, sh := range []string{"/bin/ash", "/bin/sh"} {
		if _, err := os.Stat(sh); err == nil {
			return sh
		}
	}
	return "sh"
}

// open starts a shell session and begins streaming its output.
func (m *termManager) open(ctx context.Context, msg wireMsg) {
	fail := func(err error) {
		m.log.Warn("terminal open failed", "term", msg.TermID, "err", err)
		m.send(wireMsg{Type: msgTermClose, TermID: msg.TermID, Reason: err.Error()})
	}
	if msg.TermID == "" {
		fail(fmt.Errorf("term_id is required"))
		return
	}
	m.mu.Lock()
	if len(m.sessions) >= maxTerminals {
		m.mu.Unlock()
		fail(fmt.Errorf("too many concurrent terminals (max %d)", maxTerminals))
		return
	}
	if _, dup := m.sessions[msg.TermID]; dup {
		m.mu.Unlock()
		fail(fmt.Errorf("terminal %s already open", msg.TermID))
		return
	}
	m.mu.Unlock()

	ptmx, tty, err := openPTY()
	if err != nil {
		fail(err)
		return
	}
	if msg.Cols > 0 && msg.Rows > 0 {
		unix.IoctlSetWinsize(int(ptmx.Fd()), unix.TIOCSWINSZ,
			&unix.Winsize{Col: msg.Cols, Row: msg.Rows})
	}

	sessCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(sessCtx, shellPath(), "-i")
	cmd.Env = append(os.Environ(), "TERM=xterm", "PS1=\\w # ")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &unix.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		cancel()
		ptmx.Close()
		tty.Close()
		fail(fmt.Errorf("start shell: %w", err))
		return
	}
	tty.Close() // the child holds its own copy now

	sess := &termSession{id: msg.TermID, ptmx: ptmx, cmd: cmd, cancel: cancel, lastActive: time.Now()}
	m.mu.Lock()
	m.sessions[msg.TermID] = sess
	m.mu.Unlock()
	m.log.Info("terminal opened", "term", msg.TermID, "shell", cmd.Path)

	// Output pump: pty → channel.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				sess.touch()
				data := make([]byte, n)
				copy(data, buf[:n])
				m.send(wireMsg{Type: msgTermData, TermID: sess.id, Data: data})
			}
			if err != nil {
				m.close(sess.id, "shell exited")
				return
			}
		}
	}()

	// Idle reaper.
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-sessCtx.Done():
				return
			case <-t.C:
				if time.Since(sess.idleSince()) > termIdleTimeout {
					m.close(sess.id, "idle timeout")
					return
				}
			}
		}
	}()
}

// data writes server-sent input into the pty.
func (m *termManager) data(msg wireMsg) {
	m.mu.Lock()
	sess := m.sessions[msg.TermID]
	m.mu.Unlock()
	if sess == nil {
		return
	}
	sess.touch()
	if msg.Cols > 0 && msg.Rows > 0 { // in-band resize
		unix.IoctlSetWinsize(int(sess.ptmx.Fd()), unix.TIOCSWINSZ,
			&unix.Winsize{Col: msg.Cols, Row: msg.Rows})
	}
	if len(msg.Data) > 0 {
		sess.ptmx.Write(msg.Data)
	}
}

// close tears one session down and notifies the server (idempotent).
func (m *termManager) close(id, reason string) {
	m.mu.Lock()
	sess := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if sess == nil {
		return
	}
	sess.cancel()
	sess.ptmx.Close()
	if sess.cmd.Process != nil {
		sess.cmd.Process.Kill()
	}
	go sess.cmd.Wait() // reap
	m.log.Info("terminal closed", "term", id, "reason", reason)
	m.send(wireMsg{Type: msgTermClose, TermID: id, Reason: reason})
}

// closeAll ends every session — called when the channel drops.
func (m *termManager) closeAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.close(id, "management channel closed")
	}
}
