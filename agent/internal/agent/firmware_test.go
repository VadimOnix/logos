package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubSysupgrade puts a fake sysupgrade on PATH and captures the exec call.
func stubSysupgrade(t *testing.T) (calls *[][]string, wait func()) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "sysupgrade")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var mu sync.Mutex
	got := [][]string{}
	done := make(chan struct{}, 1)
	oldExec, oldDelay, oldPath := execSysupgrade, firmwareFlashDelay, firmwareImagePath
	execSysupgrade = func(bin string, args []string) error {
		mu.Lock()
		got = append(got, append([]string{bin}, args...))
		mu.Unlock()
		done <- struct{}{}
		return nil
	}
	firmwareFlashDelay = 10 * time.Millisecond
	firmwareImagePath = filepath.Join(dir, "image.img")
	t.Cleanup(func() { execSysupgrade, firmwareFlashDelay, firmwareImagePath = oldExec, oldDelay, oldPath })
	return &got, func() {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("sysupgrade was never executed")
		}
	}
}

func TestFirmwareUpgrade(t *testing.T) {
	calls, wait := stubSysupgrade(t)
	image := []byte("firmware-image-bytes")
	sum := sha256.Sum256(image)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(image)
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]string{"url": srv.URL, "sha256": hex.EncodeToString(sum[:])})
	res, err := handleFirmwareUpgrade(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if m := res.(map[string]string); m["status"] != "flashing" {
		t.Errorf("result: %v", m)
	}
	wait()
	if len(*calls) != 1 || !strings.HasSuffix((*calls)[0][0], "sysupgrade") {
		t.Fatalf("sysupgrade calls: %v", *calls)
	}
	// keep_config default: no -n flag; last arg is the image path.
	args := (*calls)[0][1:]
	if len(args) != 1 || args[0] != firmwareImagePath {
		t.Errorf("args: %v", args)
	}
	if data, err := os.ReadFile(firmwareImagePath); err != nil || string(data) != string(image) {
		t.Errorf("image on disk: %q err=%v", data, err)
	}
}

func TestFirmwareUpgradeHashMismatch(t *testing.T) {
	calls, _ := stubSysupgrade(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("corrupted"))
	}))
	defer srv.Close()

	params, _ := json.Marshal(map[string]string{
		"url": srv.URL, "sha256": strings.Repeat("a", 64)})
	if _, err := handleFirmwareUpgrade(context.Background(), params); err == nil ||
		!strings.Contains(err.Error(), "refusing to flash") {
		t.Fatalf("hash mismatch not fatal: %v", err)
	}
	if _, err := os.Stat(firmwareImagePath); !os.IsNotExist(err) {
		t.Error("bad image left on disk")
	}
	time.Sleep(50 * time.Millisecond)
	if len(*calls) != 0 {
		t.Error("sysupgrade ran despite hash mismatch")
	}
}

func TestFirmwareUpgradeValidation(t *testing.T) {
	stubSysupgrade(t)
	for _, bad := range []map[string]string{
		{"url": "ftp://x/img", "sha256": strings.Repeat("a", 64)}, // scheme
		{"url": "https://x/img", "sha256": "nothex"},              // sha format
		{"url": "https://x/img", "sha256": ""},
	} {
		params, _ := json.Marshal(bad)
		if _, err := handleFirmwareUpgrade(context.Background(), params); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestFirmwareKeepConfigFalse(t *testing.T) {
	calls, wait := stubSysupgrade(t)
	image := []byte("img")
	sum := sha256.Sum256(image)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(image)
	}))
	defer srv.Close()

	params := fmt.Appendf(nil, `{"url":%q,"sha256":%q,"keep_config":false}`, srv.URL, hex.EncodeToString(sum[:]))
	if _, err := handleFirmwareUpgrade(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	wait()
	args := (*calls)[0][1:]
	if len(args) != 2 || args[0] != "-n" {
		t.Errorf("args: %v (want -n <image>)", args)
	}
}
