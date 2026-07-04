// Package adopt implements the adoption tool (PRD F12): it hands an existing
// OpenWrt router over to management by driving it locally over SSH. The
// router's admin credentials are used only for this session and are never
// transmitted to or stored on the control plane.
package adopt

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Router is an SSH session to the device being adopted.
type Router struct {
	client *ssh.Client
}

// Dial connects to the router. Exactly like first-time `ssh` use, the host
// key is not pre-verified — adoption is a one-shot local-LAN operation; the
// fingerprint is printed so the operator can eyeball it.
func Dial(addr, user, password, keyFile string, printFingerprint func(string)) (*Router, error) {
	if !strings.Contains(addr, ":") {
		addr += ":22"
	}
	var auth []ssh.AuthMethod
	if keyFile != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("read ssh key: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse ssh key: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if password != "" {
		auth = append(auth, ssh.Password(password))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("no SSH credentials given (need --password or --key)")
	}

	cfg := &ssh.ClientConfig{
		User: user,
		Auth: auth,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			if printFingerprint != nil {
				printFingerprint(ssh.FingerprintSHA256(key))
			}
			return nil
		},
		Timeout: 15 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh %s@%s: %w", user, addr, err)
	}
	return &Router{client: client}, nil
}

func (r *Router) Close() { r.client.Close() }

// Run executes a command and returns its combined output; non-zero exit is
// an error carrying the output.
func (r *Router) Run(cmd string) (string, error) {
	sess, err := r.client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(cmd)
	if err != nil {
		return string(out), fmt.Errorf("`%s`: %w: %s", cmd, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Upload writes data to a file on the router through stdin — no sftp
// subsystem required (dropbear on OpenWrt often lacks it).
func (r *Router) Upload(path string, data []byte, mode string) error {
	sess, err := r.client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	sess.Stdin = bytes.NewReader(data)
	cmd := fmt.Sprintf("cat > %s && chmod %s %s", path, mode, path)
	if out, err := sess.CombinedOutput(cmd); err != nil {
		return fmt.Errorf("upload %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}
