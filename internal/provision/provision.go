package provision

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agentagotchi.local/agentagotchi/internal/config"
)

type Options struct {
	DataDir     string
	HostName    string
	Port        int
	SSID        string
	Password    string
	SerialPort  string
	FirmwareDir string
	IDFPython   string
	TempUnit    string
	SkipFlash   bool
}

type message struct {
	Type       string `json:"type"`
	Version    int    `json:"version"`
	SSID       string `json:"ssid"`
	Password   string `json:"password"`
	BridgeHost string `json:"bridgeHost"`
	BridgePort int    `json:"bridgePort"`
	Token      string `json:"token"`
	CAPEM      string `json:"caPem"`
	TempUnit   string `json:"tempUnit"`
	UnixTime   int64  `json:"unixTime"`
}

func Run(opts Options) error {
	if strings.TrimSpace(opts.SSID) == "" {
		return errors.New("--ssid is required")
	}
	if opts.TempUnit == "" {
		opts.TempUnit = "F"
	}
	opts.TempUnit = strings.ToUpper(opts.TempUnit)
	if opts.TempUnit != "F" && opts.TempUnit != "C" {
		return errors.New("--temp-unit must be F or C")
	}
	if opts.FirmwareDir == "" {
		opts.FirmwareDir = "firmware"
	}
	id, err := config.EnsureIdentity(opts.DataDir, opts.HostName, opts.Port)
	if err != nil {
		return err
	}
	cert, err := config.CertificatePEM(id)
	if err != nil {
		return err
	}
	if opts.SerialPort == "" {
		opts.SerialPort, err = findSerialPort()
		if err != nil {
			return err
		}
	}
	if !strings.HasPrefix(opts.SerialPort, "/dev/cu.") && !strings.HasPrefix(opts.SerialPort, "/dev/tty.") {
		return errors.New("serial port must be under /dev/cu.* or /dev/tty.*")
	}
	if !opts.SkipFlash {
		if err := flash(opts); err != nil {
			return err
		}
		time.Sleep(3 * time.Second)
	}
	payload, err := json.Marshal(message{
		Type: "provision", Version: 1, SSID: opts.SSID, Password: opts.Password,
		BridgeHost: id.HostName, BridgePort: id.Port, Token: id.Token,
		CAPEM: cert, TempUnit: opts.TempUnit, UnixTime: time.Now().Unix(),
	})
	if err != nil {
		return err
	}
	line := append([]byte("AGOT_PROVISION "), payload...)
	line = append(line, '\r', '\n')
	if err := configureSerial(opts.SerialPort); err != nil {
		return err
	}
	f, err := os.OpenFile(opts.SerialPort, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open serial port: %w", err)
	}
	/* Opening the ESP32-S3 USB Serial/JTAG port can reset the target. Keep the
	   descriptor open and wait for app_main to reach its provisioning loop
	   before sending the one-shot configuration line. */
	time.Sleep(3 * time.Second)
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("send provisioning data: %w", err)
	}
	/*
		Keep the descriptor open until the firmware confirms that it parsed and
		persisted the record. A successful tty write only means macOS accepted
		the bytes; closing immediately can discard an outstanding USB transfer.
	*/
	ack := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "provisioning accepted") {
				ack <- nil
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ack <- err
			return
		}
		ack <- errors.New("serial port closed before device acknowledgement")
	}()
	select {
	case ackErr := <-ack:
		if ackErr != nil {
			_ = f.Close()
			return fmt.Errorf("wait for provisioning acknowledgement: %w", ackErr)
		}
	case <-time.After(8 * time.Second):
		_ = f.Close()
		return errors.New("device did not acknowledge provisioning within 8 seconds")
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close serial port: %w", err)
	}
	return nil
}

func flash(opts Options) error {
	idf := opts.IDFPython
	if idf == "" {
		var err error
		idf, err = exec.LookPath("idf.py")
		if err != nil {
			return errors.New("idf.py not found; install and activate ESP-IDF 5.5.5, or use --skip-flash")
		}
	}
	absFirmware, err := filepath.Abs(opts.FirmwareDir)
	if err != nil {
		return err
	}
	cmd := exec.Command(idf, "-p", opts.SerialPort, "build", "flash")
	cmd.Dir = absFirmware
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build and flash firmware: %w", err)
	}
	return nil
}

func configureSerial(port string) error {
	cmd := exec.Command("/bin/stty", "-f", port, "115200", "raw", "-echo", "-hupcl")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("configure serial port: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func findSerialPort() (string, error) {
	patterns := []string{
		"/dev/cu.usbmodem*", "/dev/cu.usbserial*", "/dev/cu.SLAB_USBtoUART*",
	}
	var matches []string
	for _, pattern := range patterns {
		found, _ := filepath.Glob(pattern)
		matches = append(matches, found...)
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return "", errors.New("no ESP32 serial port found; connect the BOX-3 or pass --serial")
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple serial ports found; pass --serial explicitly: %s", strings.Join(matches, ", "))
	}
}
