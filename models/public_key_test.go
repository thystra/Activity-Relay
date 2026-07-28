package models

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func rsaPublicKeysEqual(left *rsa.PublicKey, right *rsa.PublicKey) bool {
	return left != nil &&
		right != nil &&
		left.E == right.E &&
		left.N.Cmp(right.N) == 0
}

func TestRelayActorPublishesSubjectPublicKeyInfo(t *testing.T) {
	actor := NewActivityPubActorFromRelayConfig(globalConfig)

	if actor.PublicKey.ID != actor.ID+"#main-key" {
		t.Fatalf(
			"public key ID = %q; want %q",
			actor.PublicKey.ID,
			actor.ID+"#main-key",
		)
	}
	if actor.PublicKey.Owner != actor.ID {
		t.Fatalf(
			"public key owner = %q; want %q",
			actor.PublicKey.Owner,
			actor.ID,
		)
	}

	block, rest := pem.Decode([]byte(actor.PublicKey.PublicKeyPem))
	if block == nil {
		t.Fatal("relay actor publicKeyPem is not valid PEM")
	}
	if len(rest) != 0 {
		t.Fatalf("relay actor publicKeyPem has %d trailing bytes", len(rest))
	}
	if block.Type != "PUBLIC KEY" {
		t.Fatalf(
			"publicKeyPem block type = %q; want PUBLIC KEY",
			block.Type,
		)
	}

	keyInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("publicKeyPem is not SubjectPublicKeyInfo: %v", err)
	}
	publicKey, ok := keyInterface.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("publicKeyPem contains %T; want *rsa.PublicKey", keyInterface)
	}
	if !rsaPublicKeysEqual(publicKey, &globalConfig.actorKey.PublicKey) {
		t.Fatal("published public key does not match actor.pem")
	}
}

func TestReadPublicKeyRSAFromStringAcceptsPKIXAndPKCS1(t *testing.T) {
	publicKey := &globalConfig.actorKey.PublicKey
	pkixBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("marshal PKIX key: %v", err)
	}

	tests := []struct {
		name     string
		pemBlock *pem.Block
	}{
		{
			name: "SubjectPublicKeyInfo",
			pemBlock: &pem.Block{
				Type:  "PUBLIC KEY",
				Bytes: pkixBytes,
			},
		},
		{
			name: "legacy PKCS1",
			pemBlock: &pem.Block{
				Type:  "RSA PUBLIC KEY",
				Bytes: x509.MarshalPKCS1PublicKey(publicKey),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := ReadPublicKeyRSAFromString(
				string(pem.EncodeToMemory(test.pemBlock)),
			)
			if err != nil {
				t.Fatalf("read public key: %v", err)
			}
			if !rsaPublicKeysEqual(parsed, publicKey) {
				t.Fatal("parsed public key does not match actor.pem")
			}
		})
	}
}

func TestReadPublicKeyRSAFromStringRejectsInvalidPEM(t *testing.T) {
	if _, err := ReadPublicKeyRSAFromString("not a PEM public key"); err == nil {
		t.Fatal("invalid public key PEM was accepted")
	}
}
