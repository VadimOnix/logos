// logos-adopt hands an existing OpenWrt router over to Logos management
// (PRD F12) and takes it back out (PRD §4.4) — driving the device locally
// over SSH. Admin credentials never leave this machine.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/VadimOnix/logos/agent/internal/adopt"
)

const usage = `logos-adopt — adopt an existing OpenWrt router into Logos

Usage:
  logos-adopt run    --router <host[:port]> --server <url> --code <claim-code>
                     [--user root] [--password <pw> | --key <file>]
                     [--agent-binary <path>] [--force]
  logos-adopt remove --router <host[:port]> [--user root] [--password | --key]
                     [--cleanup] [--yes]

Credentials are used only for the local SSH session; they are never sent to
the control plane. ` + "`remove --cleanup`" + ` restores the pre-adoption snapshot
(removes packages installed since adoption, reverts UCI configuration).

Environment:
  LOGOS_SSH_PASSWORD   SSH password (alternative to --password / prompt)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	router := fs.String("router", "", "router address, host[:port]")
	user := fs.String("user", "root", "ssh user")
	password := fs.String("password", os.Getenv("LOGOS_SSH_PASSWORD"), "ssh password (omit to be prompted)")
	keyFile := fs.String("key", "", "ssh private key file")
	server := fs.String("server", "", "control plane URL")
	code := fs.String("code", "", "claim code from the panel")
	agentBinary := fs.String("agent-binary", "", "local logos-agent binary (otherwise downloaded from the control plane)")
	force := fs.Bool("force", false, "adopt even if the compatibility check fails")
	cleanup := fs.Bool("cleanup", false, "remove: also restore the pre-adoption snapshot")
	yes := fs.Bool("yes", false, "remove: skip the cleanup confirmation")

	var err error
	switch cmd {
	case "run", "remove":
		fs.Parse(args)
		if *router == "" {
			fmt.Fprintln(os.Stderr, "error: --router is required")
			os.Exit(2)
		}
		if *password == "" && *keyFile == "" {
			if *password, err = promptPassword(*user, *router); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
		}
		opts := adopt.Options{
			RouterAddr: *router, User: *user, Password: *password, KeyFile: *keyFile,
			Server: *server, Code: *code, AgentBinary: *agentBinary, Force: *force,
		}
		if cmd == "run" {
			err = adopt.Adopt(ctx, opts, os.Stdout)
		} else {
			err = adopt.Remove(opts, *cleanup, *yes, os.Stdout)
		}
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func promptPassword(user, router string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no --password/--key given and stdin is not a terminal")
	}
	fmt.Fprintf(os.Stderr, "%s@%s password: ", user, router)
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(pw), err
}
