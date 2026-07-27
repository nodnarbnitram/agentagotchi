package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const DefaultPort = 8787

type Identity struct {
	Token     string `json:"token"`
	HostName  string `json:"hostName"`
	Port      int    `json:"port"`
	CertPath  string `json:"certPath"`
	KeyPath   string `json:"keyPath"`
	CreatedAt string `json:"createdAt"`
}

func DefaultDataDir() string {
	if v := os.Getenv("CODEX_PET_DATA_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".codex-pet"
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "CodexPet")
	}
	return filepath.Join(home, ".local", "share", "codex-pet")
}

func DefaultHostName() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("/usr/sbin/scutil", "--get", "LocalHostName").Output(); err == nil {
			if v := normalizeHost(string(out)); v != "" {
				return v + ".local"
			}
		}
	}
	if host, err := os.Hostname(); err == nil {
		if v := normalizeHost(host); v != "" {
			if !strings.Contains(v, ".") {
				v += ".local"
			}
			return v
		}
	}
	return "codex-pet.local"
}

func EnsureIdentity(dataDir, hostName string, port int) (Identity, error) {
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	if hostName == "" {
		hostName = DefaultHostName()
	}
	hostName = normalizeHost(hostName)
	if port == 0 {
		port = DefaultPort
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return Identity{}, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return Identity{}, fmt.Errorf("secure data directory: %w", err)
	}

	identityPath := filepath.Join(dataDir, "identity.json")
	var id Identity
	if b, err := os.ReadFile(identityPath); err == nil {
		if err := json.Unmarshal(b, &id); err != nil {
			return Identity{}, fmt.Errorf("decode identity: %w", err)
		}
	}
	if id.Token == "" {
		token := make([]byte, 32)
		if _, err := rand.Read(token); err != nil {
			return Identity{}, fmt.Errorf("create device token: %w", err)
		}
		id.Token = hex.EncodeToString(token)
	}
	if id.HostName == "" {
		id.HostName = hostName
	}
	if id.Port == 0 {
		id.Port = port
	}
	id.CertPath = filepath.Join(dataDir, "bridge-cert.pem")
	id.KeyPath = filepath.Join(dataDir, "bridge-key.pem")
	if id.CreatedAt == "" {
		id.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if !certificateProfileValid(id.CertPath, id.KeyPath, id.HostName, time.Now()) {
		if err := generateCertificate(id.CertPath, id.KeyPath, id.HostName); err != nil {
			return Identity{}, err
		}
	}
	b, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return Identity{}, err
	}
	if err := os.WriteFile(identityPath, b, 0o600); err != nil {
		return Identity{}, fmt.Errorf("write identity: %w", err)
	}
	for _, path := range []string{identityPath, id.CertPath, id.KeyPath} {
		if err := os.Chmod(path, 0o600); err != nil {
			return Identity{}, fmt.Errorf("secure identity file %s: %w", filepath.Base(path), err)
		}
	}
	return id, nil
}

func CertificatePEM(id Identity) (string, error) {
	b, err := os.ReadFile(id.CertPath)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func generateCertificate(certPath, keyPath, hostName string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate TLS key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return err
	}
	now := time.Now()
	names := uniqueStrings([]string{
		hostName, strings.TrimSuffix(hostName, ".local"),
		"codex-pet.local", "localhost",
	})
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   hostName,
			Organization: []string{"Codex Pet Local Bridge"},
		},
		NotBefore: now.Add(-time.Hour),
		NotAfter:  now.AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature |
			x509.KeyUsageKeyEncipherment |
			x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              names,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create TLS certificate: %w", err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}), 0o600); err != nil {
		return err
	}
	return nil
}

func certificateProfileValid(certPath, keyPath, hostName string, now time.Time) bool {
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil || len(pair.Certificate) == 0 {
		return false
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return false
	}
	return cert.IsCA &&
		cert.BasicConstraintsValid &&
		cert.KeyUsage&x509.KeyUsageCertSign != 0 &&
		now.After(cert.NotBefore) &&
		now.Before(cert.NotAfter) &&
		cert.VerifyHostname(hostName) == nil
}

func normalizeHost(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimSuffix(v, ".")
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-.")
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
