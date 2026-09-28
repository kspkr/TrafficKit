// Package discovery lets other local processes (the MCP server, the CLI)
// find a running engine.
//
// The engine writes <dataDir>/engine.json with its API address and token.
// The file is readable only by the current user: on Unix it's mode 0600, and
// on Windows it lives under %AppData%, which is private to the user. Anyone
// who can read it could already read the CA key next to it.
package discovery

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const fileName = "engine.json"

type Info struct {
	API     string `json:"api"`
	Token   string `json:"token"`
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

var ErrNotRunning = errors.New("TrafficKit isn't running")

func Path(dataDir string) string { return filepath.Join(dataDir, fileName) }

func Write(dataDir string, info Info) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	tmp := Path(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path(dataDir))
}

func Read(dataDir string) (Info, error) {
	data, err := os.ReadFile(Path(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return Info{}, ErrNotRunning
	}
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil || info.API == "" || info.Token == "" {
		return Info{}, ErrNotRunning
	}
	return info, nil
}

// Remove deletes the file if it still belongs to pid; another engine may
// have started since and taken it over.
func Remove(dataDir string, pid int) {
	if info, err := Read(dataDir); err == nil && info.PID == pid {
		os.Remove(Path(dataDir))
	}
}
