package launch

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// Reveal shows path in the system file manager, selected where the platform
// supports it. path always comes from the engine itself, never from a client.
func Reveal(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", "/select,", path)
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
