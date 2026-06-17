package conf

import (
	"os/exec"
	"strings"
	"time"
)

var (
	Version   = "v2.1.4"
	Commit    = "unknown"
	BuildTime = "unknown"
	Author    = "GGGZERO"
	Repo      = "https://github.com/gypg/ggzero"
)

func init() {
	if Commit == "unknown" {
		if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
			Commit = strings.TrimSpace(string(out))
		}
	}
	if BuildTime == "unknown" {
		BuildTime = time.Now().UTC().Format(time.RFC3339)
	}
}
