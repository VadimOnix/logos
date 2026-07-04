package adopt

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/VadimOnix/logos/agent/internal/initscript"
)

const (
	agentPath = "/usr/bin/logos-agent"
	initPath  = "/etc/init.d/logos-agent"
)

// Options configures an adoption run.
type Options struct {
	RouterAddr string // host[:port]
	User       string
	Password   string
	KeyFile    string

	Server string // control-plane URL
	Code   string // claim code

	AgentBinary string // local path; downloaded from the server when empty
	Force       bool
}

// Adopt drives the full flow of PRD §4.1: connect locally over SSH, verify
// compatibility, snapshot, install the agent, enroll. Mid-install failures
// roll back everything that was uploaded.
func Adopt(ctx context.Context, opts Options, out io.Writer) error {
	if opts.Server == "" || opts.Code == "" {
		return fmt.Errorf("--server and --code are required")
	}
	if _, err := url.Parse(opts.Server); err != nil {
		return fmt.Errorf("invalid --server URL: %w", err)
	}

	step := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }

	step("connecting to %s@%s …", opts.User, opts.RouterAddr)
	r, err := Dial(opts.RouterAddr, opts.User, opts.Password, opts.KeyFile,
		func(fp string) { step("  host key fingerprint: %s", fp) })
	if err != nil {
		return err
	}
	defer r.Close()

	step("checking device compatibility …")
	info, err := Detect(r)
	if err != nil {
		return err
	}
	step("  %s · %s · arch %s · %d MB RAM · %d KB free flash · %s",
		info.Hostname, orUnknown(info.OSPretty), info.GoArch,
		info.MemTotalKB/1024, info.FlashFreeKB, orUnknown(info.PkgManager))
	if err := info.CheckCompatibility(opts.Force); err != nil {
		return err
	}
	if already, _ := r.Run("test -f /etc/logos/agent.json && echo yes; true"); strings.Contains(already, "yes") {
		return fmt.Errorf("device is already enrolled (state at /etc/logos/agent.json); run `logos-adopt remove` first")
	}

	step("taking pre-adoption snapshot (%d-second op) …", 5)
	snap, err := TakeSnapshot(r, info.PkgManager)
	if err != nil {
		return err
	}
	snapJSON, err := snap.JSON()
	if err != nil {
		return err
	}

	binary, err := agentBinary(ctx, opts, info.GoArch)
	if err != nil {
		return err
	}
	step("installing logos-agent (%d KB) …", len(binary)/1024)

	// Everything uploaded from here on is rolled back on failure.
	var installed []string
	rollback := func() {
		for _, p := range installed {
			r.Run("rm -f " + p)
		}
	}
	upload := func(path string, data []byte, mode string) error {
		if err := r.Upload(path, data, mode); err != nil {
			rollback()
			return err
		}
		installed = append(installed, path)
		return nil
	}

	if _, err := r.Run("mkdir -p /etc/logos"); err != nil {
		return err
	}
	if err := upload(SnapshotPath, snapJSON, "600"); err != nil {
		return err
	}
	if err := upload(agentPath, binary, "755"); err != nil {
		return err
	}
	if info.HasProcd {
		if err := upload(initPath, []byte(initscript.Script), "755"); err != nil {
			return err
		}
	}

	step("enrolling against %s …", opts.Server)
	enrollOut, err := r.Run(fmt.Sprintf("%s enroll --server %q --code %q", agentPath, opts.Server, opts.Code))
	if err != nil {
		rollback()
		r.Run("rm -f /etc/logos/agent.json")
		return fmt.Errorf("enrollment failed (device rolled back): %w", err)
	}
	fmt.Fprint(out, indent(enrollOut))

	step("starting the agent …")
	if info.HasProcd {
		if _, err := r.Run(initPath + " enable && " + initPath + " start"); err != nil {
			return fmt.Errorf("agent installed and enrolled, but the service failed to start: %w", err)
		}
	} else {
		// Non-procd device (useful for lab/testing): background the agent.
		if _, err := r.Run(fmt.Sprintf("nohup %s run >/tmp/logos-agent.log 2>&1 & sleep 1", agentPath)); err != nil {
			return fmt.Errorf("agent installed and enrolled, but failed to start: %w", err)
		}
	}

	step("done — the node should appear online in the panel within seconds.")
	step("credentials used for this session were not sent anywhere; snapshot stored at %s", SnapshotPath)
	return nil
}

// Remove offboards a previously adopted device from the operator's machine
// (PRD §4.4: works even when the control plane is unreachable *from the
// router* — the leave runs locally on the device).
func Remove(opts Options, cleanup, yes bool, out io.Writer) error {
	r, err := Dial(opts.RouterAddr, opts.User, opts.Password, opts.KeyFile, nil)
	if err != nil {
		return err
	}
	defer r.Close()

	args := ""
	if cleanup {
		args = " --cleanup"
		if yes {
			args += " --yes"
		}
	}
	leaveOut, err := r.Run(agentPath + " leave" + args)
	fmt.Fprint(out, indent(leaveOut))
	if err != nil {
		return err
	}
	// Remove what adoption installed. The agent handles its own state.
	if _, err := r.Run(fmt.Sprintf("rm -f %s %s", initPath, agentPath)); err != nil {
		return err
	}
	fmt.Fprintln(out, "agent removed from the device")
	return nil
}

// agentBinary loads the agent binary for the target architecture — from a
// local file when given, otherwise from the control plane.
func agentBinary(ctx context.Context, opts Options, goarch string) ([]byte, error) {
	if opts.AgentBinary != "" {
		return os.ReadFile(opts.AgentBinary)
	}
	dlURL := strings.TrimRight(opts.Server, "/") + "/api/v1/agent-binary/" + goarch
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, dlURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download agent from control plane: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("control plane has no agent binary for %s (%s); pass --agent-binary <path>", goarch, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func indent(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return "  " + strings.ReplaceAll(s, "\n", "\n  ") + "\n"
}
