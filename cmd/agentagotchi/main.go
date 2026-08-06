package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"agentagotchi.local/agentagotchi/internal/adapters/codex"
	"agentagotchi.local/agentagotchi/internal/config"
	"agentagotchi.local/agentagotchi/internal/edge"
	"agentagotchi.local/agentagotchi/internal/pairing"
	"agentagotchi.local/agentagotchi/internal/provision"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agentagotchi:", err)
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
	case "pair":
		return runPair(args[1:])
	case "status":
		return runStatus(args[1:])
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
	homeURL := fs.String("home-url", "", "optional Home Bridge wss:// URL (Edge→Home upstream)")
	homeToken := fs.String("home-token", "", "edge-ingress pairing credential for --home-url")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	opts := edge.Options{
		DataDir: *dataDir, HostName: *host, Port: *port, CodexBinary: *codexBin,
		DisableMDNS: *noMDNS, DisableAppServer: *noAppServer,
		Logger: log.New(os.Stderr, "agentagotchi: ", log.LstdFlags),
	}
	if *homeURL != "" {
		if *homeToken == "" {
			return fmt.Errorf("--home-url requires --home-token (pair with 'agentagotchi pair' first)")
		}
		opts.Upstream = &edge.UpstreamConfig{URL: *homeURL, Token: *homeToken}
	}
	return edge.Serve(ctx, opts)
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
	event, err := codex.Sanitize(os.Stdin, time.Now())
	if err != nil {
		return nil // Hook telemetry is best-effort and must never disrupt Codex.
	}
	if *socket == "" {
		*socket = filepath.Join(*dataDir, "edge.sock")
	}
	_ = edge.SendHook(*socket, event)
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
	fmt.Fprintln(os.Stderr, `Usage: agentagotchi <command> [options]

Commands:
  serve       Run the Edge Bridge
  status      Show Edge status (connectivity, pairings, Task Presence counts)
  hook        Receive one Codex hook event from stdin
  pair        Manage the Pairing Ceremony (begin/approve/deny/pending/list/revoke)
  provision   Build, flash, and provision the BOX-3
  version     Print the Edge version`)
}

// runStatus prints privacy-safe Edge status from the owner-only admin IPC:
// connectivity, pairing counts, Task Presence counts — never prompts,
// transcripts, tool payloads, commands, or filesystem metadata.
func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	dataDir := fs.String("data-dir", config.DefaultDataDir(), "Edge data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	socket := filepath.Join(*dataDir, "edge.sock")
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return fmt.Errorf("Edge IPC: %w (is 'agentagotchi serve' running?)", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	request := map[string]string{"schema": "agentagotchi.admin.v1", "type": "status"}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return err
	}
	var reply struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		Status *struct {
			Role           string    `json:"role"`
			Version        string    `json:"version"`
			StartedAt      time.Time `json:"startedAt"`
			PairedDevices  int       `json:"pairedDevices"`
			ConnectedPeers int       `json:"connectedPeers"`
			TaskPresences  int       `json:"taskPresences"`
			AggregateState string    `json:"aggregateState"`
		} `json:"status"`
	}
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return err
	}
	if !reply.OK || reply.Status == nil {
		return fmt.Errorf("%s", reply.Error)
	}
	s := reply.Status
	fmt.Printf("Edge %s (v%s), up since %s\n", s.Role, s.Version, s.StartedAt.Format(time.RFC3339))
	fmt.Printf("aggregate: %s · task presences: %d · paired: %d · connected: %d\n",
		s.AggregateState, s.TaskPresences, s.PairedDevices, s.ConnectedPeers)
	return nil
}

// runPair drives the owner-only administration IPC for the Pairing Ceremony.
// Credential tokens print only on explicit redeem, on the connecting client's
// own invocation — never in list/status output.
func runPair(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("pair requires a subcommand: begin|approve|deny|list|pending|revoke|redeem")
	}
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	dataDir := fs.String("data-dir", config.DefaultDataDir(), "Edge data directory")
	role := fs.String("role", "feed", "pairing role: feed|edge-ingress")
	clientName := fs.String("client", "", "connecting client name")
	codeID := fs.String("code", "", "pairing code ID")
	credentialID := fs.String("credential", "", "credential ID")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	socket := filepath.Join(*dataDir, "edge.sock")
	sub := args[0]
	request := map[string]string{"schema": "agentagotchi.admin.v1", "type": "pairing_" + sub}
	switch sub {
	case "begin":
		request["role"], request["clientName"] = *role, *clientName
	case "approve", "deny":
		request["codeId"] = *codeID
	case "revoke":
		request["credentialId"] = *credentialID
	case "list", "pending":
	default:
		return fmt.Errorf("unknown pair subcommand %q", sub)
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return fmt.Errorf("Edge IPC: %w (is 'agentagotchi serve' running?)", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return err
	}
	var reply struct {
		OK          bool                 `json:"ok"`
		Error       string               `json:"error"`
		Code        *pairing.Code        `json:"code"`
		Pending     []pairing.Code       `json:"pending"`
		Credentials []pairing.Credential `json:"credentials"`
	}
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return err
	}
	if !reply.OK {
		return fmt.Errorf("%s", reply.Error)
	}
	switch sub {
	case "begin":
		fmt.Printf("Pairing code for %s: %s\n", reply.Code.ClientName, reply.Code.Token)
		fmt.Printf("Code ID %s, expires %s\n", reply.Code.ID, reply.Code.ExpiresAt.Format(time.RFC3339))
	case "pending":
		for _, code := range reply.Pending {
			fmt.Printf("%s  role=%s client=%s expires=%s\n", code.ID, code.Role, code.ClientName, code.ExpiresAt.Format(time.RFC3339))
		}
	case "list":
		for _, cred := range reply.Credentials {
			state := "active"
			if cred.Revoked {
				state = "revoked"
			}
			fmt.Printf("%s  role=%s client=%s issued=%s %s\n", cred.ID, cred.Role, cred.ClientName, cred.IssuedAt.Format(time.RFC3339), state)
		}
	default:
		fmt.Println("ok")
	}
	return nil
}
