package i18n

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// osLanguage reads the first preferred language of macOS, for the
// processes started without LANG (launchd, some terminals).
func osLanguage() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleLocale").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
