package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// F2: first-run setup portal. A pre-flashed (or freshly reset) device has no
// enrollment state; instead of exiting, the agent serves a small local page
// where whoever set the router up enters the control-plane URL and a claim
// code. Successful enrollment shuts the portal down and starts the normal
// management channel.
//
// The portal only ever runs while the device is UNENROLLED — the moment
// state exists it is unreachable, so it grants nothing an attacker on the
// LAN could not already get from an unconfigured router.

const portalPage = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Logos setup</title>
<style>
  body { margin: 0 auto; max-width: 420px; padding: 40px 16px; font: 15px/1.5 system-ui, sans-serif; color: #1a1d21; }
  h1 { font-size: 20px; margin-bottom: 4px; }
  .muted { color: #667085; font-size: 13px; }
  form { display: flex; flex-direction: column; gap: 10px; margin-top: 20px; }
  input { font: inherit; border: 1px solid #e4e7ec; border-radius: 6px; padding: 9px 10px; }
  button { font: inherit; background: #175cd3; border: 0; color: #fff; border-radius: 6px; padding: 10px; cursor: pointer; }
  .error { color: #b42318; font-size: 13px; white-space: pre-wrap; }
  .ok { color: #12805c; }
</style>
<h1>Logos setup</h1>
<div class="muted">{{.Hostname}} · {{.Arch}} · agent {{.Version}}</div>
{{if .Enrolled}}
  <p class="ok"><strong>Enrolled.</strong> This device is now managed by {{.Server}} —
  it should appear online in the panel within a few seconds. This setup page is closed.</p>
{{else}}
  <p class="muted">Connect this router to a Logos control plane. Create a claim code
  in the panel (single-use, expires in 1&nbsp;hour) and enter it below.</p>
  <form method="post" action="/enroll">
    <input name="server" placeholder="control plane URL, e.g. https://logos.example.com" value="{{.Server}}" required>
    <input name="code" placeholder="claim code, e.g. LG-XXXXX-XXXXX" value="" required autocomplete="off">
    <button type="submit">Enroll this device</button>
    <div class="error">{{.Error}}</div>
  </form>
{{end}}
`

var portalTmpl = template.Must(template.New("portal").Parse(portalPage))

type portalData struct {
	Hostname string
	Arch     string
	Version  string
	Server   string
	Error    string
	Enrolled bool
}

// portal is the setup HTTP server. enroll is injectable for tests.
type portal struct {
	statePath string
	log       *slog.Logger
	enroll    func(ctx context.Context, statePath, server, code string) error

	mu       sync.Mutex
	lastSrv  string
	lastErr  string
	enrolled bool
	done     chan struct{} // closed on successful enrollment
}

func newPortal(statePath string, log *slog.Logger) *portal {
	return &portal{statePath: statePath, log: log, enroll: Enroll, done: make(chan struct{})}
}

func (p *portal) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// Anything else (including captive-portal probes) lands on the
			// setup page.
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		p.render(w)
	})
	mux.HandleFunc("POST /enroll", func(w http.ResponseWriter, r *http.Request) {
		server, code := r.FormValue("server"), r.FormValue("code")
		p.mu.Lock()
		p.lastSrv, p.lastErr = server, ""
		alreadyDone := p.enrolled
		p.mu.Unlock()
		if alreadyDone {
			p.render(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		err := p.enroll(ctx, p.statePath, server, code)
		cancel()
		p.mu.Lock()
		if err != nil {
			p.lastErr = err.Error()
			p.log.Warn("portal enrollment failed", "err", err)
		} else if !p.enrolled {
			p.enrolled = true
			close(p.done)
		}
		p.mu.Unlock()
		p.render(w)
	})
	return mux
}

func (p *portal) render(w http.ResponseWriter) {
	hostname, _ := os.Hostname()
	p.mu.Lock()
	d := portalData{
		Hostname: hostname,
		Arch:     runtime.GOARCH,
		Version:  Version,
		Server:   p.lastSrv,
		Error:    p.lastErr,
		Enrolled: p.enrolled,
	}
	p.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	portalTmpl.Execute(w, d)
}

// preseedPath is the enrollment preseed a baked image ships next to the
// state file (F14): {"server": ..., "code": ...}. Single-use — removed after
// a successful enrollment (the claim code is single-use anyway).
func preseedPath(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), "preseed.json")
}

// tryPreseed attempts one auto-enrollment from the preseed file, if present.
// Returns true when the device is now enrolled.
func (p *portal) tryPreseed(ctx context.Context) bool {
	data, err := os.ReadFile(preseedPath(p.statePath))
	if err != nil {
		return false
	}
	var ps struct {
		Server string `json:"server"`
		Code   string `json:"code"`
	}
	if err := json.Unmarshal(data, &ps); err != nil || ps.Server == "" || ps.Code == "" {
		p.log.Warn("ignoring malformed preseed file", "path", preseedPath(p.statePath))
		return false
	}
	enrollCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	err = p.enroll(enrollCtx, p.statePath, ps.Server, ps.Code)
	cancel()
	if err != nil {
		// Normal early in first boot: WAN may not be up yet. Keep retrying.
		p.log.Warn("preseed enrollment attempt failed", "server", ps.Server, "err", err)
		return false
	}
	os.Remove(preseedPath(p.statePath))
	p.mu.Lock()
	if !p.enrolled {
		p.enrolled = true
		close(p.done)
	}
	p.mu.Unlock()
	p.log.Info("auto-enrolled from preseed", "server", ps.Server)
	return true
}

// servePortal blocks until enrollment succeeds (or ctx is done).
func servePortal(ctx context.Context, statePath, addr string, log *slog.Logger) error {
	p := newPortal(statePath, log)
	srv := &http.Server{Addr: addr, Handler: p.handler(), ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Info("setup portal listening — open it in a browser to enroll this device", "addr", addr)

	// A baked image (F14) auto-enrolls in the background while the portal
	// serves; whichever succeeds first wins.
	if _, err := os.Stat(preseedPath(statePath)); err == nil {
		go func() {
			for !p.tryPreseed(ctx) {
				select {
				case <-ctx.Done():
					return
				case <-p.done:
					return
				case <-time.After(15 * time.Second):
				}
			}
		}()
	}

	select {
	case err := <-errCh:
		return fmt.Errorf("setup portal: %w", err)
	case <-ctx.Done():
		srv.Close()
		return ctx.Err()
	case <-p.done:
		// Give the browser a beat to receive the success page before the
		// listener goes away.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		srv.Shutdown(shutdownCtx)
		cancel()
		return nil
	}
}

// RunWithPortal is the daemon entry point for devices that may not be
// enrolled yet (PRD F2 pre-flashed flow): serve the setup portal until
// enrollment, then run the management channel; if the node later leaves
// (panel removal wipes the state), fall back to the portal again.
func RunWithPortal(ctx context.Context, statePath, portalAddr string, log *slog.Logger) error {
	for ctx.Err() == nil {
		if _, err := LoadState(statePath); err != nil {
			if err := servePortal(ctx, statePath, portalAddr, log); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
		}
		if err := Run(ctx, statePath, log); err != nil {
			return err
		}
		// Run returned cleanly: either ctx is done (loop exits) or the node
		// left management and the state was wiped — back to the portal.
	}
	return nil
}
