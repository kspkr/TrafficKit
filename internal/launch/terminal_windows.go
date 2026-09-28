package launch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const (
	createNewConsole         = 0x00000010
	createUnicodeEnvironment = 0x00000400
)

func terminalTarget() (Target, bool) {
	shell, name := "pwsh.exe", "PowerShell 7"
	path, err := exec.LookPath(shell)
	if err != nil {
		shell, name = "powershell.exe", "Windows PowerShell"
		if path, err = exec.LookPath(shell); err != nil {
			return Target{}, false
		}
	}
	return Target{
		ID:          "terminal",
		Kind:        "terminal",
		Name:        "New terminal",
		Description: name + " with proxy and certificate variables set for curl, Node, Python, git and more",
		Path:        path,
	}, true
}

func terminalCommand(t Target, env Env, _ string) (starter, bool, func(), error) {
	bundle, err := writeBundle(env)
	if err != nil {
		return nil, false, nil, err
	}
	vars := TerminalEnv(env, bundle)
	if home, err := writeCurlConfig(env); err == nil {
		vars["CURL_HOME"] = home
	}
	// ProxyAddr is host:port from our own listener, so it's safe to embed.
	banner := "$Host.UI.RawUI.WindowTitle = 'TrafficKit terminal'; " +
		"Write-Host 'TrafficKit is capturing HTTP and HTTPS from this terminal (proxy " + env.ProxyAddr + ").' -ForegroundColor Yellow; " +
		"Write-Host 'Programs started here send their traffic through it. Close the window to stop.' -ForegroundColor DarkGray"
	args := []string{t.Path, "-NoExit", "-NoLogo", "-Command", banner}
	environ := mergeEnv(vars)
	return func() (*os.Process, error) { return startConsole(args, environ) }, true, func() {}, nil
}

// startConsole starts a console program in a new window of its own.
//
// os/exec can't do this: it always passes standard handles, and with none
// set that means NUL. PowerShell then reads its commands from NUL, hits end
// of file and exits, even with -NoExit. Calling CreateProcess without
// STARTF_USESTDHANDLES lets the new console provide real ones.
func startConsole(args []string, env []string) (*os.Process, error) {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = syscall.EscapeArg(a)
	}
	cmdline, err := syscall.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		return nil, err
	}
	block := envBlock(env)
	si := syscall.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi syscall.ProcessInformation
	err = syscall.CreateProcess(nil, cmdline, nil, nil, false,
		createNewConsole|createUnicodeEnvironment, &block[0], nil, &si, &pi)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(pi.Thread)
	defer syscall.CloseHandle(pi.Process)
	// FindProcess opens its own handle, which Wait and Kill use.
	return os.FindProcess(int(pi.ProcessId))
}

// envBlock encodes env as a CreateProcess environment block: NUL-separated
// UTF-16 strings, terminated by an extra NUL.
func envBlock(env []string) []uint16 {
	var b []uint16
	for _, kv := range env {
		b = append(b, utf16.Encode([]rune(kv))...)
		b = append(b, 0)
	}
	return append(b, 0)
}

// The curl.exe that ships with Windows uses Schannel, which ignores
// CURL_CA_BUNDLE and also insists on a revocation check our certificates
// can't answer. Both can be set from a config file, which curl finds through
// CURL_HOME. This replaces the user's own .curlrc inside this terminal only.
func writeCurlConfig(env Env) (string, error) {
	dir := filepath.Join(env.DataDir, "launch", "curl")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	conf := fmt.Sprintf("cacert = %q\nssl-revoke-best-effort\n", filepath.ToSlash(env.CAPath))
	for _, name := range []string{".curlrc", "_curlrc"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(conf), 0o600); err != nil {
			return "", err
		}
	}
	return dir, nil
}
