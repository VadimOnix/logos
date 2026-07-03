package hub

import "testing"

type fakeConn struct {
	closed bool
	left   string
}

func (c *fakeConn) SendLeave(reason string) { c.left = reason }
func (c *fakeConn) Close()                  { c.closed = true }

func TestRegisterReplacesStaleConnection(t *testing.T) {
	h := New()
	old, fresh := &fakeConn{}, &fakeConn{}

	h.Register("n1", old)
	if !h.IsOnline("n1") {
		t.Fatal("node not online after register")
	}
	h.Register("n1", fresh)
	if !old.closed {
		t.Error("stale connection was not closed on replacement")
	}

	// Unregister of the stale conn must not disconnect the fresh one.
	h.Unregister("n1", old)
	if !h.IsOnline("n1") {
		t.Error("fresh connection dropped by stale unregister")
	}
	h.Unregister("n1", fresh)
	if h.IsOnline("n1") {
		t.Error("node still online after unregister")
	}
}

func TestKick(t *testing.T) {
	h := New()
	c := &fakeConn{}
	h.Register("n1", c)
	h.Kick("n1", "removed")
	if h.IsOnline("n1") {
		t.Error("node online after kick")
	}
	if c.left != "removed" || !c.closed {
		t.Errorf("kick did not send leave+close: left=%q closed=%v", c.left, c.closed)
	}
	h.Kick("missing", "noop") // must not panic
}
