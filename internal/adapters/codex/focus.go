package codex

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
)

// Codex-generated native thread IDs are currently UUIDv7. Accept every UUID
// version defined by RFC 9562 while retaining exact shape and variant checks.
var uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func ValidThreadID(id string) bool { return uuidRE.MatchString(id) }

type CommandRunner func(name string, args ...string) error

func defaultCommandRunner(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

// FocusHandler returns an exact Codex focus capability handler. The input is
// the Edge-private native session ID resolved by presence.Core, never the
// public Task Presence ID received from a feed.
func FocusHandler(run CommandRunner) func(string) error {
	if run == nil {
		run = defaultCommandRunner
	}
	return func(nativeSessionID string) error {
		return focus(runtime.GOOS, nativeSessionID, run)
	}
}

func focus(goos, nativeSessionID string, run CommandRunner) error {
	if goos != "darwin" {
		return errors.New("Codex focus is supported on macOS only")
	}
	if !ValidThreadID(nativeSessionID) {
		return fmt.Errorf("invalid Codex thread UUID")
	}
	// Deliberately no `open -b com.openai.codex` fallback: exact thread open or
	// failure is the complete capability contract (docs/adr/0006).
	return run("/usr/bin/open", "codex://threads/"+nativeSessionID)
}
