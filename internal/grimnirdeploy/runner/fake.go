/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package runner

import (
	"context"
	"strings"
	"sync"
)

// Call captures one Runner.Run invocation.
type Call struct {
	Host string
	Cmd  string
}

// FakeResponse is what the fake returns for a matched command.
type FakeResponse struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// Fake is an in-memory Runner for tests. It records every call and looks up
// the response by exact-command match first, then by prefix.
type Fake struct {
	mu       sync.Mutex
	Calls    []Call
	exact    map[string]FakeResponse
	prefixes []prefixResp
}

type prefixResp struct {
	prefix string
	resp   FakeResponse
}

// NewFake constructs an empty Fake.
func NewFake() *Fake {
	return &Fake{exact: map[string]FakeResponse{}}
}

// SetResponse registers an exact-match response.
func (f *Fake) SetResponse(cmd, stdout, stderr string, code int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exact[cmd] = FakeResponse{stdout, stderr, code, err}
}

// SetResponsePrefix registers a prefix-match response. Falls through to exact-
// match if the cmd is not in the exact table.
func (f *Fake) SetResponsePrefix(prefix, stdout, stderr string, code int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prefixes = append(f.prefixes, prefixResp{prefix, FakeResponse{stdout, stderr, code, err}})
}

// Run records the call and returns the matched response.
func (f *Fake) Run(ctx context.Context, host, cmd string) (string, string, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Host: host, Cmd: cmd})
	if r, ok := f.exact[cmd]; ok {
		return r.Stdout, r.Stderr, r.ExitCode, r.Err
	}
	for _, p := range f.prefixes {
		if strings.HasPrefix(cmd, p.prefix) {
			return p.resp.Stdout, p.resp.Stderr, p.resp.ExitCode, p.resp.Err
		}
	}
	return "", "", 0, nil
}
