package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-fed/httpsig"
	"github.com/thystra/Activity-Relay/models"
)

func decodeActivity(request *http.Request) (*models.Activity, *models.Actor, []byte, error) {
	request.Header.Set("Host", request.Host)
	body, err := io.ReadAll(io.LimitReader(request.Body, GlobalConfig.MaxActivityBytes()+1))
	if err != nil {
		return nil, nil, nil, err
	}
	if int64(len(body)) > GlobalConfig.MaxActivityBytes() {
		return nil, nil, nil, errors.New("activity body exceeds configured limit")
	}

	// Verify HTTPSignature
	verifier, err := httpsig.NewVerifier(request)
	if err != nil {
		return nil, nil, nil, err
	}
	KeyID := verifier.KeyId()
	keyOwnerActor, err := models.NewActivityPubActorFromRemoteActor(KeyID, fmt.Sprintf("%s (golang net/http; Activity-Relay %s; %s)", GlobalConfig.ServerServiceName(), version, GlobalConfig.ServerHostname().Host), ActorCache)
	if err != nil {
		return nil, nil, nil, err
	}
	PubKey, err := models.ReadPublicKeyRSAFromString(keyOwnerActor.PublicKey.PublicKeyPem)
	if PubKey == nil {
		return nil, nil, nil, errors.New("failed parse PublicKey from string")
	}
	if err != nil {
		return nil, nil, nil, err
	}
	err = verifier.Verify(PubKey, httpsig.RSA_SHA256)
	if err != nil {
		return nil, nil, nil, err
	}

	// Verify Digest
	givenDigest := request.Header.Get("Digest")
	hash := sha256.New()
	hash.Write(body)
	b := hash.Sum(nil)
	calculatedDigest := "SHA-256=" + base64.StdEncoding.EncodeToString(b)

	if givenDigest != calculatedDigest {
		return nil, nil, nil, errors.New("digest header is mismatch")
	}

	// Parse Activity
	var activity models.Activity
	err = json.Unmarshal(body, &activity)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := verifySignatureActorBinding(KeyID, keyOwnerActor, activity.Actor); err != nil {
		return nil, nil, nil, err
	}
	remoteActor, err := models.NewActivityPubActorFromRemoteActor(activity.Actor, fmt.Sprintf("%s (golang net/http; Activity-Relay %s; %s)", GlobalConfig.ServerServiceName(), version, GlobalConfig.ServerHostname().Host), ActorCache)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := verifyActivityActorDocument(activity.Actor, remoteActor); err != nil {
		return nil, nil, nil, err
	}

	return &activity, &remoteActor, body, nil
}

func normalizedRemoteHost(address string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" {
		return "", errors.New("remote URL has no host")
	}
	return host, nil
}

func verifySignatureActorBinding(keyID string, keyOwner models.Actor, activityActor string) error {
	actorHost, err := normalizedRemoteHost(activityActor)
	if err != nil {
		return fmt.Errorf("invalid activity actor: %w", err)
	}
	keyHost, err := normalizedRemoteHost(keyID)
	if err != nil {
		return fmt.Errorf("invalid signature key ID: %w", err)
	}
	if keyHost != actorHost {
		return errors.New("signature key host does not match activity actor host")
	}
	if keyOwner.ID != "" {
		ownerHost, err := normalizedRemoteHost(keyOwner.ID)
		if err != nil || ownerHost != actorHost {
			return errors.New("signature key owner does not match activity actor host")
		}
	}
	if keyOwner.PublicKey.Owner != "" {
		ownerHost, err := normalizedRemoteHost(keyOwner.PublicKey.Owner)
		if err != nil || ownerHost != actorHost {
			return errors.New("public key owner does not match activity actor host")
		}
	}
	return nil
}

func verifyActivityActorDocument(activityActor string, remoteActor models.Actor) error {
	activityHost, err := normalizedRemoteHost(activityActor)
	if err != nil {
		return fmt.Errorf("invalid activity actor: %w", err)
	}
	remoteHost, err := normalizedRemoteHost(remoteActor.ID)
	if err != nil || remoteHost != activityHost {
		return errors.New("resolved actor document does not match activity actor host")
	}
	return nil
}

func fetchOriginalActivityFromURL(url string) (*models.Activity, *models.Actor, error) {
	remoteActivity, err := models.NewActivityPubActivityFromRemoteActivity(url, fmt.Sprintf("%s (golang net/http; Activity-Relay %s; %s)", GlobalConfig.ServerServiceName(), version, GlobalConfig.ServerHostname().Host))
	if err != nil {
		return nil, nil, err
	}
	remoteActor, err := models.NewActivityPubActorFromRemoteActor(remoteActivity.Actor, fmt.Sprintf("%s (golang net/http; Activity-Relay %s; %s)", GlobalConfig.ServerServiceName(), version, GlobalConfig.ServerHostname().Host), ActorCache)
	if err != nil {
		return &remoteActivity, nil, err
	}
	return &remoteActivity, &remoteActor, err
}
