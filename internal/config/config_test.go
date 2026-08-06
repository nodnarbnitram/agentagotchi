package config

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureIdentityCreatesPinnedCAProfileAndPrivateFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := EnsureIdentity(dir, "agentagotchi.local", 6571)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(id.CertPath, id.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if !cert.IsCA || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Fatalf("certificate cannot act as the provisioned trust anchor: %+v", cert.KeyUsage)
	}
	if err := cert.VerifyHostname("agentagotchi.local"); err != nil {
		t.Fatal(err)
	}
	if !certificateProfileValid(id.CertPath, id.KeyPath, id.HostName, time.Now()) {
		t.Fatal("generated certificate profile rejected")
	}
	for _, path := range []string{
		dir, filepath.Join(dir, "identity.json"), id.CertPath, id.KeyPath,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
		}
	}
}
