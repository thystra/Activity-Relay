package models

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// GenerateActorKey creates a PKCS#1 RSA relay identity without replacing an
// existing key. The caller remains responsible for setting the final owner.
func GenerateActorKey(path string, bits int) error {
	if bits < 2048 {
		return errors.New("RSA key size must be at least 2048 bits")
	}
	if path == "" {
		return errors.New("actor key output path is empty")
	}

	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return fmt.Errorf("generate RSA key: %w", err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("create actor key directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("actor key already exists: %s", path)
		}
		return fmt.Errorf("create actor key: %w", err)
	}
	complete := false
	defer func() {
		file.Close()
		if !complete {
			os.Remove(path)
		}
	}()

	if _, err := file.Write(encoded); err != nil {
		return fmt.Errorf("write actor key: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync actor key: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close actor key: %w", err)
	}
	complete = true
	return nil
}
