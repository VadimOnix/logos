package main

import (
	"context"
	"fmt"
	"os"

	"github.com/VadimOnix/logos/agent/internal/adopt"
)

type fleetArgs struct {
	server, apiToken string
	csvFile, ipRange string
	user, password   string
	keyFile          string
	agentBinary      string
	force            bool
	concurrency      int
}

func runFleet(ctx context.Context, a fleetArgs) error {
	if a.server == "" {
		return fmt.Errorf("--server is required")
	}
	if a.apiToken == "" {
		return fmt.Errorf("--api-token (or LOGOS_API_TOKEN) is required — fleet mode mints a claim code per router")
	}
	if (a.csvFile == "") == (a.ipRange == "") {
		return fmt.Errorf("provide exactly one of --csv or --range")
	}

	var targets []adopt.Target
	var err error
	if a.csvFile != "" {
		targets, err = adopt.LoadCSVFile(a.csvFile)
	} else {
		targets, err = adopt.ParseRange(a.ipRange)
	}
	if err != nil {
		return err
	}

	// A password prompt makes no sense across many hosts; require an explicit
	// credential (or per-row CSV passwords).
	if a.password == "" && a.keyFile == "" && a.csvFile == "" {
		return fmt.Errorf("fleet mode needs --password, --key, or per-row CSV credentials (no interactive prompt)")
	}

	minter := adopt.NewCodeMinter(a.server, a.apiToken)
	fmt.Fprintf(os.Stderr, "adopting %d router(s), %d at a time …\n", len(targets), a.concurrency)

	_, err = adopt.AdoptFleet(ctx, adopt.FleetOptions{
		Targets:     targets,
		User:        a.user,
		Password:    a.password,
		KeyFile:     a.keyFile,
		Server:      a.server,
		AgentBinary: a.agentBinary,
		Force:       a.force,
		Concurrency: a.concurrency,
		MintCode: func(c context.Context, router string) (string, error) {
			return minter.Mint(c, "fleet adoption: "+router)
		},
	}, os.Stdout)
	return err
}
