// Package pkgparse extracts package names from opkg/apk installed-list
// output. Shared by the adoption snapshot and the agent-side cleanup diff;
// kept dependency-free so the agent binary stays small.
package pkgparse

import "strings"

// Names returns the bare package names from `opkg list-installed` /
// `apk list --installed` output.
func Names(manager, out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "WARNING") {
			continue
		}
		switch manager {
		case "opkg": // "name - version [- description]"
			if name, _, ok := strings.Cut(line, " - "); ok {
				names = append(names, strings.TrimSpace(name))
			}
		case "apk": // "name-1.2.3-r0 arch {origin} ..."
			first, _, _ := strings.Cut(line, " ")
			if i := versionDash(first); i > 0 {
				names = append(names, first[:i])
			}
		}
	}
	return names
}

// versionDash finds the dash starting the "-<ver>-r<N>" suffix of an apk
// package spec (versions may themselves contain dashes), or -1.
func versionDash(s string) int {
	ri := strings.LastIndex(s, "-r")
	if ri <= 0 {
		return -1
	}
	vi := strings.LastIndex(s[:ri], "-")
	if vi <= 0 {
		return -1
	}
	return vi
}
