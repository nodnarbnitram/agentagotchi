package edge

import (
	"context"
	"io"
	"log"
	"os/exec"
	"strconv"
)

func advertiseMDNS(ctx context.Context, port int, logger *log.Logger) {
	path, err := exec.LookPath("dns-sd")
	if err != nil {
		logger.Printf("Bonjour advertisement skipped: dns-sd not found")
		return
	}
	cmd := exec.CommandContext(ctx, path, "-R", "Agentagotchi", "_agentagotchi._tcp", "local",
		strconv.Itoa(port), "version=1")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		logger.Printf("Bonjour advertisement stopped")
	}
}
