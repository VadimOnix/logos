// Package initscript holds the canonical procd init script for logos-agent,
// shared by every place that installs the agent outside the package system:
// the SSH adoption tool (F12) and the image builder wrapper (F14).
//
// Keep in sync with agent/openwrt/files/logos-agent.init (the copy the
// OpenWrt feed package installs).
package initscript

const Script = `#!/bin/sh /etc/rc.common
# procd service for logos-agent: with enrollment state it opens the
# management channel; without it the agent serves the first-run setup
# portal (F2, http://<router>:8484).

START=95
STOP=10
USE_PROCD=1

PROG=/usr/bin/logos-agent

start_service() {
	procd_open_instance
	procd_set_param command "$PROG" run
	procd_set_param respawn 3600 5 0   # retry forever, 5s apart
	procd_set_param stdout 1
	procd_set_param stderr 1
	procd_close_instance
}

service_triggers() {
	procd_add_reload_trigger logos
}
`
