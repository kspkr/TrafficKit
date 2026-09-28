//go:build !windows

package launch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Terminal emulators on Linux, in order of preference. Most of them hand the
// window to an existing server process, so we can't track the result.
var linuxTerminals = []string{"x-terminal-emulator", "gnome-terminal", "konsole", "xfce4-terminal", "kitty", "alacritty", "xterm"}

func terminalTarget() (Target, bool) {
	if runtime.GOOS == "darwin" {
		return Target{
			ID: "terminal", Kind: "terminal", Name: "New terminal",
			Description: "Terminal.app with proxy and certificate variables set for curl, Node, Python, git and more",
			Path:        "/usr/bin/open",
		}, true
	}
	for _, name := range linuxTerminals {
		if p, err := exec.LookPath(name); err == nil {
			return Target{
				ID: "terminal", Kind: "terminal", Name: "New terminal",
				Description: filepath.Base(p) + " with proxy and certificate variables set for curl, Node, Python, git and more",
				Path:        p,
			}, true
		}
	}
	return Target{}, false
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// The environment goes into a small script rather than the process
// environment, because terminal servers don't pass theirs on.
func terminalCommand(t Target, env Env, id string) (starter, bool, func(), error) {
	bundle, err := writeBundle(env)
	if err != nil {
		return nil, false, nil, err
	}
	vars := TerminalEnv(env, bundle)
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "export %s=%s\n", k, shellQuote(vars[k]))
	}
	fmt.Fprintf(&b, "printf '\\033[33mTrafficKit is capturing HTTP and HTTPS from this terminal (proxy %s).\\033[0m\\n'\n", env.ProxyAddr)
	b.WriteString("exec \"${SHELL:-/bin/sh}\" -l\n")

	dir := filepath.Join(env.DataDir, "launch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, nil, err
	}
	script := filepath.Join(dir, "terminal-"+id+".command")
	if err := os.WriteFile(script, []byte(b.String()), 0o700); err != nil {
		return nil, false, nil, err
	}
	cleanup := func() { os.Remove(script) }

	var cmd *exec.Cmd
	switch base := filepath.Base(t.Path); {
	case runtime.GOOS == "darwin":
		cmd = exec.Command(t.Path, "-a", "Terminal", script)
	case base == "gnome-terminal":
		cmd = exec.Command(t.Path, "--", "/bin/sh", script)
	default:
		cmd = exec.Command(t.Path, "-e", "/bin/sh", script)
	}
	return startCmd(cmd), false, cleanup, nil
}
