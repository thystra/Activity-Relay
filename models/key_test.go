package models

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateActorKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity", "actor.pem")
	if err := GenerateActorKey(path, 2048); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("key mode = %o; want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Fatal("generated key is not a PKCS#1 RSA PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if key.N.BitLen() != 2048 {
		t.Fatalf("key size = %d; want 2048", key.N.BitLen())
	}

	if err := GenerateActorKey(path, 2048); err == nil {
		t.Fatal("expected existing key to be preserved")
	}
}

func TestGenerateActorKeyRejectsWeakKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actor.pem")
	if err := GenerateActorKey(path, 1024); err == nil {
		t.Fatal("expected weak key size to be rejected")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("weak-key request created an output file")
	}
}
