package agent

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// collector is a thread-safe sink for the messages the term manager emits.
type collector struct {
	mu   sync.Mutex
	msgs []wireMsg
}

func (c *collector) send(m wireMsg) {
	c.mu.Lock()
	c.msgs = append(c.msgs, m)
	c.mu.Unlock()
}

func (c *collector) output() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	for _, m := range c.msgs {
		if m.Type == msgTermData {
			b.Write(m.Data)
		}
	}
	return b.String()
}

func (c *collector) closed() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.msgs {
		if m.Type == msgTermClose {
			return m.Reason, true
		}
	}
	return "", false
}

func testLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestTerminalEcho opens a real shell over a pty and checks a command's
// output flows back, then that closing kills the session.
func TestTerminalEcho(t *testing.T) {
	if _, _, err := openPTY(); err != nil {
		t.Skipf("no pty support in this environment: %v", err)
	}
	c := &collector{}
	m := newTermManager(testLog(), c.send)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.open(ctx, wireMsg{Type: msgTermOpen, TermID: "t1", Cols: 80, Rows: 24})
	m.data(wireMsg{Type: msgTermData, TermID: "t1", Data: []byte("echo logos-marker-42\n")})

	deadline := time.After(5 * time.Second)
	for {
		if strings.Contains(c.output(), "logos-marker-42") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("marker never echoed; got:\n%q", c.output())
		case <-time.After(50 * time.Millisecond):
		}
	}

	m.close("t1", "test done")
	if _, ok := c.closed(); !ok {
		t.Error("no term_close emitted")
	}
	// Writing after close is a no-op, not a panic.
	m.data(wireMsg{Type: msgTermData, TermID: "t1", Data: []byte("x")})
}

func TestTerminalMaxSessions(t *testing.T) {
	if _, _, err := openPTY(); err != nil {
		t.Skipf("no pty support: %v", err)
	}
	c := &collector{}
	m := newTermManager(testLog(), c.send)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer m.closeAll()

	for i := 0; i < maxTerminals; i++ {
		m.open(ctx, wireMsg{Type: msgTermOpen, TermID: string(rune('a' + i))})
	}
	// One more must be rejected.
	m.open(ctx, wireMsg{Type: msgTermOpen, TermID: "overflow"})
	reason, ok := c.closed()
	if !ok || !strings.Contains(reason, "too many") {
		t.Errorf("overflow not rejected: %q %v", reason, ok)
	}
}

func TestTerminalOpenRequiresID(t *testing.T) {
	c := &collector{}
	m := newTermManager(testLog(), c.send)
	m.open(context.Background(), wireMsg{Type: msgTermOpen})
	if reason, ok := c.closed(); !ok || !strings.Contains(reason, "term_id is required") {
		t.Errorf("missing-id not rejected: %q", reason)
	}
}
