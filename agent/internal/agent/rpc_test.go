package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

func callDispatch(t *testing.T, msg wireMsg) wireMsg {
	t.Helper()
	out := make(chan wireMsg, 1)
	sem := make(chan struct{}, 1)
	dispatchRPC(context.Background(), msg, slog.New(slog.DiscardHandler), sem, func(m wireMsg) { out <- m })
	select {
	case m := <-out:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no rpc_result within 5s")
		return wireMsg{}
	}
}

func TestDispatchPing(t *testing.T) {
	res := callDispatch(t, wireMsg{Type: msgRPC, ID: "1", Method: "ping"})
	if res.Type != msgRPCResult || res.ID != "1" || !res.OK {
		t.Fatalf("res = %+v", res)
	}
	var body map[string]string
	if err := json.Unmarshal(res.Result, &body); err != nil || body["pong"] == "" {
		t.Errorf("result = %s (err %v)", res.Result, err)
	}
}

func TestDispatchUnknownMethod(t *testing.T) {
	res := callDispatch(t, wireMsg{Type: msgRPC, ID: "2", Method: "nope.nothing"})
	if res.OK || res.Error == "" || res.ID != "2" {
		t.Fatalf("res = %+v", res)
	}
}
