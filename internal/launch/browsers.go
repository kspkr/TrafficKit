package launch

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// StartURL is the page launched browsers open when no URL is given. The
// proxy serves it itself; see proxy.LocalHost.
const StartURL = "https://traffickit.test/"

type browserSpec struct {
	id, name string
	windows  []string // relative to Program Files / LocalAppData
	darwin   []string // app bundle executables
	linux    []string // commands on PATH
}

// Chromium-based only: they all take the same flags. Firefox needs a
// prepared profile and certificate database, which isn't built yet.
var browsers = []browserSpec{
	{
		id: "chrome", name: "Google Chrome",
		windows: []string{`Google\Chrome\Application\chrome.exe`},
		darwin:  []string{"Google Chrome.app/Contents/MacOS/Google Chrome"},
		linux:   []string{"google-chrome", "google-chrome-stable"},
	},
	{
		id: "edge", name: "Microsoft Edge",
		windows: []string{`Microsoft\Edge\Application\msedge.exe`},
		darwin:  []string{"Microsoft Edge.app/Contents/MacOS/Microsoft Edge"},
		linux:   []string{"microsoft-edge", "microsoft-edge-stable"},
	},
	{
		id: "brave", name: "Brave",
		windows: []string{`BraveSoftware\Brave-Browser\Application\brave.exe`},
		darwin:  []string{"Brave Browser.app/Contents/MacOS/Brave Browser"},
		linux:   []string{"brave-browser", "brave"},
	},
	{
		id: "chromium", name: "Chromium",
		windows: []string{`Chromium\Application\chrome.exe`},
		darwin:  []string{"Chromium.app/Contents/MacOS/Chromium"},
		linux:   []string{"chromium", "chromium-browser"},
	},
	{
		id: "vivaldi", name: "Vivaldi",
		windows: []string{`Vivaldi\Application\vivaldi.exe`},
		darwin:  []string{"Vivaldi.app/Contents/MacOS/Vivaldi"},
		linux:   []string{"vivaldi", "vivaldi-stable"},
	},
}

func detectBrowsers() []Target {
	var out []Target
	for _, b := range browsers {
		if path := findBrowser(b); path != "" {
			out = append(out, Target{
				ID:          b.id,
				Kind:        "browser",
				Name:        b.name,
				Description: "A fresh window with its own temporary profile",
				Path:        path,
			})
		}
	}
	return out
}

func findBrowser(b browserSpec) string {
	switch runtime.GOOS {
	case "windows":
		var roots []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			if v := os.Getenv(env); v != "" {
				roots = append(roots, v)
			}
		}
		for _, root := range roots {
			for _, rel := range b.windows {
				if p := filepath.Join(root, rel); isFile(p) {
					return p
				}
			}
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		for _, root := range []string{"/Applications", filepath.Join(home, "Applications")} {
			for _, rel := range b.darwin {
				if p := filepath.Join(root, rel); isFile(p) {
					return p
				}
			}
		}
	default:
		for _, name := range b.linux {
			if p, err := exec.LookPath(name); err == nil {
				return p
			}
		}
	}
	return ""
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// BrowserArgs builds the Chromium command line for a proxied, isolated
// window.
func BrowserArgs(profile string, env Env, url string) []string {
	if url == "" {
		url = StartURL
	}
	return []string{
		"--user-data-dir=" + profile,
		"--proxy-server=http://" + env.ProxyAddr,
		// Chromium skips the proxy for localhost by default; debugging a
		// local dev server is a main use case, so don't.
		"--proxy-bypass-list=<-loopback>",
		// Trust the TrafficKit CA in this instance only. Chromium honours
		// this flag only together with a custom --user-data-dir.
		"--ignore-certificate-errors-spki-list=" + env.SPKI,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-search-engine-choice-screen",
		url,
	}
}
