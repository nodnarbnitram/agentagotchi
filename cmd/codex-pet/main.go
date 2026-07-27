package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"codexpet.local/codex-pet/internal/bridge"
	"codexpet.local/codex-pet/internal/config"
	"codexpet.local/codex-pet/internal/hook"
	"codexpet.local/codex-pet/internal/provision"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "codex-pet:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return flag.ErrHelp
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "hook":
		return runHook(args[1:])
	case "provision":
		return runProvision(args[1:])
	case "version", "--version", "-version":
		fmt.Println(version)
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dataDir := fs.String("data-dir", config.DefaultDataDir(), "bridge data directory")
	host := fs.String("host-name", config.DefaultHostName(), "Bonjour/manual bridge hostname")
	port := fs.Int("port", config.DefaultPort, "WSS listen port")
	codexBin := fs.String("codex-bin", "", "Codex CLI path for read-only metadata")
	noMDNS := fs.Bool("no-mdns", false, "disable Bonjour advertisement")
	noAppServer := fs.Bool("no-app-server", false, "disable read-only App Server metadata")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return bridge.Serve(ctx, bridge.Options{
		DataDir: *dataDir, HostName: *host, Port: *port, CodexBinary: *codexBin,
		DisableMDNS: *noMDNS, DisableAppServer: *noAppServer,
		Logger: log.New(os.Stderr, "codex-pet: ", log.LstdFlags),
	})
}

func runHook(args []string) error {
	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	dataDir := fs.String("data-dir", config.DefaultDataDir(), "bridge data directory")
	socket := fs.String("socket", "", "override bridge Unix socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Codex Stop/SubagentStop hooks require JSON stdout on success. An empty
	// object is valid for every event and never alters Codex behavior.
	defer func() {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{})
	}()
	event, err := hook.Sanitize(os.Stdin, time.Now())
	if err != nil {
		return nil // Hook telemetry is best-effort and must never disrupt Codex.
	}
	if *socket == "" {
		*socket = filepath.Join(*dataDir, "bridge.sock")
	}
	_ = bridge.SendHook(*socket, event)
	return nil
}

func runProvision(args []string) error {
	fs := flag.NewFlagSet("provision", flag.ContinueOnError)
	dataDir := fs.String("data-dir", config.DefaultDataDir(), "bridge data directory")
	host := fs.String("host-name", config.DefaultHostName(), "bridge hostname")
	port := fs.Int("port", config.DefaultPort, "bridge WSS port")
	ssid := fs.String("ssid", "", "Wi-Fi SSID")
	password := fs.String("password", "", "Wi-Fi password")
	passwordStdin := fs.Bool("password-stdin", false, "read Wi-Fi password from stdin (avoids command history)")
	serial := fs.String("serial", "", "BOX-3 serial device")
	firmware := fs.String("firmware", "firmware", "firmware directory")
	idf := fs.String("idf", "", "idf.py path")
	tempUnit := fs.String("temp-unit", "F", "temperature unit: F or C")
	skipFlash := fs.Bool("skip-flash", false, "only send configuration")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *passwordStdin {
		if *password != "" {
			return fmt.Errorf("--password and --password-stdin are mutually exclusive")
		}
		value, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && len(value) == 0 {
			return fmt.Errorf("read Wi-Fi password from stdin: %w", err)
		}
		*password = strings.TrimSuffix(strings.TrimSuffix(value, "\n"), "\r")
	}
	err := provision.Run(provision.Options{
		DataDir: *dataDir, HostName: *host, Port: *port, SSID: *ssid,
		Password: *password, SerialPort: *serial, FirmwareDir: *firmware,
		IDFPython: *idf, TempUnit: *tempUnit, SkipFlash: *skipFlash,
	})
	if err == nil {
		fmt.Println("Provisioning sent; the BOX-3 will join Wi-Fi.")
	}
	return err
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: codex-pet <command> [options]

Commands:
  serve       Run the macOS bridge
  hook        Receive one Codex hook event from stdin
  provision   Build, flash, and provision the BOX-3
  version     Print the bridge version`)
}
