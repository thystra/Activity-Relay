package models

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/redis/go-redis/v9"
)

func ReadPublicKeyRSAFromString(pemString string) (*rsa.PublicKey, error) {
	decoded, _ := pem.Decode([]byte(pemString))
	if decoded == nil {
		return nil, errors.New("public key PEM is invalid")
	}

	keyInterface, pkixErr := x509.ParsePKIXPublicKey(decoded.Bytes)
	if pkixErr == nil {
		publicKey, ok := keyInterface.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf(
				"public key is %T, not RSA",
				keyInterface,
			)
		}
		return publicKey, nil
	}

	publicKey, pkcs1Err := x509.ParsePKCS1PublicKey(decoded.Bytes)
	if pkcs1Err == nil {
		return publicKey, nil
	}

	return nil, fmt.Errorf(
		"parse RSA public key: PKIX: %v; PKCS#1: %v",
		pkixErr,
		pkcs1Err,
	)
}

func redisHGetOrCreateWithDefault(redisClient *redis.Client, key string, field string, defaultValue string) (string, error) {
	keyExist, err := redisClient.HExists(context.TODO(), key, field).Result()
	if err != nil {
		return "", err
	}
	if keyExist {
		value, err := redisClient.HGet(context.TODO(), key, field).Result()
		if err != nil {
			return "", err
		}
		return value, nil
	} else {
		_, err := redisClient.HSet(context.TODO(), key, field, defaultValue).Result()
		if err != nil {
			return "", err
		}
		return defaultValue, nil
	}
}

func readPrivateKeyRSA(keyPath string) (*rsa.PrivateKey, error) {
	file, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	decoded, _ := pem.Decode(file)
	if decoded == nil {
		return nil, errors.New("ACTOR_PEM IS INVALID. FAILED TO READ")
	}
	privateKey, err := x509.ParsePKCS1PrivateKey(decoded.Bytes)
	if err != nil {
		return nil, err
	}
	return privateKey, nil
}

func generatePublicKeyPEMString(publicKey *rsa.PublicKey) string {
	if publicKey == nil {
		return ""
	}
	publicKeyByte, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return ""
	}
	publicKeyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: publicKeyByte,
		},
	)
	return string(publicKeyPEM)
}
