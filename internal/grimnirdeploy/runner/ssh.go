/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHRunner runs commands locally (via os/exec) for host=="local" or via SSH
// for any other host. SSH clients are cached per host.
type SSHRunner struct {
	user     string
	port     int
	keyPath  string
	mu       sync.Mutex
	clients  map[string]*ssh.Client // host -> client
	hostKeys ssh.HostKeyCallback    // production: use known_hosts; tests: substitute
}

// NewSSHRunner constructs an SSH runner. The keyPath points at a PEM-encoded
// private key. hostKeyCallback is the verification callback; pass
// ssh.InsecureIgnoreHostKey() ONLY in tests.
func NewSSHRunner(user string, port int, keyPath string, hostKeys ssh.HostKeyCallback) *SSHRunner {
	return &SSHRunner{
		user: user, port: port, keyPath: keyPath, hostKeys: hostKeys,
		clients: map[string]*ssh.Client{},
	}
}

// Run executes cmd on host. host=="local" runs via os/exec.
func (r *SSHRunner) Run(ctx context.Context, host, cmd string) (string, string, int, error) {
	if host == "local" {
		return runLocal(ctx, cmd)
	}
	c, err := r.client(host)
	if err != nil {
		return "", "", 0, fmt.Errorf("ssh dial %s: %w", host, err)
	}
	sess, err := c.NewSession()
	if err != nil {
		return "", "", 0, fmt.Errorf("ssh session %s: %w", host, err)
	}
	defer sess.Close()
	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGTERM)
		return stdout.String(), stderr.String(), -1, ctx.Err()
	case runErr := <-done:
		if runErr == nil {
			return stdout.String(), stderr.String(), 0, nil
		}
		var ee *ssh.ExitError
		if errors.As(runErr, &ee) {
			return stdout.String(), stderr.String(), ee.ExitStatus(), nil
		}
		return stdout.String(), stderr.String(), -1, runErr
	}
}

// Close tears down all cached SSH clients.
func (r *SSHRunner) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.clients {
		_ = c.Close()
	}
	r.clients = map[string]*ssh.Client{}
}

func (r *SSHRunner) client(host string) (*ssh.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[host]; ok {
		return c, nil
	}
	key, err := os.ReadFile(r.keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse key: %w", err)
	}
	cb := r.hostKeys
	if cb == nil {
		cb = ssh.InsecureIgnoreHostKey() // only reached if caller forgot to pass one
	}
	conf := &ssh.ClientConfig{
		User:            r.user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: cb,
		Timeout:         10 * time.Second,
	}
	addr := net.JoinHostPort(host, strconv.Itoa(r.port))
	c, err := ssh.Dial("tcp", addr, conf)
	if err != nil {
		return nil, err
	}
	r.clients[host] = c
	return c, nil
}

func runLocal(ctx context.Context, cmd string) (string, string, int, error) {
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	var out, errBuf bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errBuf
	err := c.Run()
	if err == nil {
		return out.String(), errBuf.String(), 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), errBuf.String(), ee.ExitCode(), nil
	}
	return out.String(), errBuf.String(), -1, err
}
