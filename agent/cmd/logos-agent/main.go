// logos-agent is the Logos daemon for OpenWrt nodes (PRD F1): it enrolls the
// device into a control plane, keeps an outbound management channel open,
// reports heartbeats, and can cleanly leave management at any time.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/VadimOnix/logos/agent/internal/agent"
)

const usage = `logos-agent — Logos node agent

Usage:
  logos-agent enroll --server <url> --code <claim-code> [--state <path>]
  logos-agent run    [--state <path>]
  logos-agent leave  [--state <path>]
  logos-agent status [--state <path>]
  logos-agent version

Environment:
  LOGOS_AGENT_STATE   state file path (default ` + agent.DefaultStatePath + `)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "enroll":
		fs := flag.NewFlagSet("enroll", flag.ExitOnError)
		server := fs.String("server", "", "control plane URL, e.g. https://logos.example.com")
		code := fs.String("code", "", "claim code from the panel")
		state := fs.String("state", agent.StatePath(), "state file path")
		fs.Parse(args)
		err = agent.Enroll(ctx, *state, *server, *code)
	case "run":
		fs := flag.NewFlagSet("run", flag.ExitOnError)
		state := fs.String("state", agent.StatePath(), "state file path")
		fs.Parse(args)
		log := slog.New(slog.NewTextHandler(os.Stderr, nil))
		err = agent.Run(ctx, *state, log)
	case "leave":
		fs := flag.NewFlagSet("leave", flag.ExitOnError)
		state := fs.String("state", agent.StatePath(), "state file path")
		fs.Parse(args)
		err = agent.Leave(ctx, *state)
	case "status":
		fs := flag.NewFlagSet("status", flag.ExitOnError)
		state := fs.String("state", agent.StatePath(), "state file path")
		fs.Parse(args)
		st, lerr := agent.LoadState(*state)
		if lerr != nil {
			fmt.Printf("not enrolled (%v)\n", lerr)
			os.Exit(1)
		}
		fmt.Printf("enrolled\n  server: %s\n  node:   %s\n  state:  %s\n", st.ServerURL, st.NodeID, *state)
	case "version":
		fmt.Println(agent.Version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
