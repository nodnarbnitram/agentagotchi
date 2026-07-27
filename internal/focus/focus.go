package focus

import (
	"errors"
	"os/exec"
	"regexp"
	"runtime"
)

// Codex-generated task IDs are currently UUIDv7. Accept every UUID version
// defined by RFC 9562 so legacy task IDs remain focusable, while retaining the
// variant and exact-shape checks that keep untrusted input out of the URL.
var uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func ValidThreadID(id string) bool {
	return uuidRE.MatchString(id)
}

func OpenThread(id string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("task focusing is currently supported on macOS only")
	}
	if ValidThreadID(id) {
		if err := exec.Command("/usr/bin/open", "codex://threads/"+id).Run(); err == nil {
			return nil
		}
	}
	return exec.Command("/usr/bin/open", "-b", "com.openai.codex").Run()
}
