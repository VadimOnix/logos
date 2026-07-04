package agent

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

// Wireless associations via ubus iwinfo (F6). Only available on OpenWrt;
// silently absent elsewhere. Uses the ubus CLI instead of the socket
// protocol to stay dependency-free.

type WifiClient struct {
	Device    string `json:"device"`
	MAC       string `json:"mac"`
	SignalDBm int    `json:"signal_dbm,omitempty"`
}

func collectWirelessClients() []WifiClient {
	bin, err := exec.LookPath("ubus")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, "call", "iwinfo", "devices").Output()
	if err != nil {
		return nil
	}
	var devs struct {
		Devices []string `json:"devices"`
	}
	if err := json.Unmarshal(out, &devs); err != nil {
		return nil
	}

	var clients []WifiClient
	for _, dev := range devs.Devices {
		arg, _ := json.Marshal(map[string]string{"device": dev})
		out, err := exec.CommandContext(ctx, bin, "call", "iwinfo", "assoclist", string(arg)).Output()
		if err != nil {
			continue
		}
		var res struct {
			Results []struct {
				MAC    string `json:"mac"`
				Signal int    `json:"signal"`
			} `json:"results"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			continue
		}
		for _, r := range res.Results {
			clients = append(clients, WifiClient{Device: dev, MAC: r.MAC, SignalDBm: r.Signal})
		}
	}
	return clients
}
