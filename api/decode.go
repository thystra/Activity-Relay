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

const (
	inboundSignatureProfileLegacy  = "legacy"
	inboundSignatureProfileRFC9421 = "rfc9421"
)

func selectInboundSignatureProfile(request *http.Request) string {
	if request != nil &&
		len(request.Header.Values("Signature-Input")) != 0 {
		return inboundSignatureProfileRFC9421
	}
	return inboundSignatureProfileLegacy
}

func decodeActivity(
	request *http.Request,
) (*models.Activity, *models.Actor, []byte, error) {
	profile := selectInboundSignatureProfile(request)
	body, err := readInboundActivityBody(request)
	if err != nil {
		return nil, nil, nil, err
	}

	var activity *models.Activity
	var actor *models.Actor

	switch profile {
	case inboundSignatureProfileRFC9421:
		activity, actor, err = decodeRFC9421Activity(request, body)
	default:
		activity, actor, err = decodeLegacyActivity(request, body)
	}

	if err != nil {
		recordHTTPSignatureVerification(
			profile,
			"failure",
			classifyHTTPSignatureVerificationError(err),
		)
		return nil, nil, nil, err
	}

	recordHTTPSignatureVerification(profile, "success", "accepted")
	return activity, actor, body, nil
}

func readInboundActivityBody(request *http.Request) ([]byte, error) {
	if request == nil || request.Body == nil {
		return nil, errors.New("activity request body is unavailable")
	}
	body, err := io.ReadAll(
		io.LimitReader(
			request.Body,
			GlobalConfig.MaxActivityBytes()+1,
		),
	)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > GlobalConfig.MaxActivityBytes() {
		return nil, errors.New(
			"activity body exceeds configured limit",
		)
	}
	return body, nil
}

func parseInboundActivity(body []byte) (*models.Activity, error) {
	var activity models.Activity
	if err := json.Unmarshal(body, &activity); err != nil {
		return nil, err
	}
	if strings.TrimSpace(activity.Actor) == "" {
		return nil, errors.New("activity has no actor")
	}
	return &activity, nil
}

func decodeRFC9421Activity(
	request *http.Request,
	body []byte,
) (*models.Activity, *models.Actor, error) {
	if InboundRFC9421Verifier == nil {
		return nil, nil, errors.New(
			"RFC 9421 inbound verifier is unavailable",
		)
	}

	verification, err := InboundRFC9421Verifier.VerifyPOST(
		request,
		body,
	)
	if err != nil {
		return nil, nil, err
	}

	activity, err := parseInboundActivity(body)
	if err != nil {
		return nil, nil, err
	}
	if err := verification.BindActivityActor(activity.Actor); err != nil {
		return nil, nil, err
	}

	remoteActor, err := models.NewActivityPubActorFromRemoteActor(
		activity.Actor,
		relayRemoteUserAgent(),
		ActorCache,
		RemoteRequestSigner,
	)
	if err != nil {
		return nil, nil, err
	}
	if err := verification.BindActivityActor(remoteActor.ID); err != nil {
		return nil, nil, err
	}

	return activity, &remoteActor, nil
}

func decodeLegacyActivity(
	request *http.Request,
	body []byte,
) (*models.Activity, *models.Actor, error) {
	request.Header.Set("Host", request.Host)

	verifier, err := httpsig.NewVerifier(request)
	if err != nil {
		return nil, nil, err
	}
	keyID := verifier.KeyId()

	keyOwnerActor, err := models.NewActivityPubActorFromRemoteActor(
		keyID,
		relayRemoteUserAgent(),
		ActorCache,
		RemoteRequestSigner,
	)
	if err != nil {
		return nil, nil, err
	}
	publicKey, err := models.ReadPublicKeyRSAFromString(
		keyOwnerActor.PublicKey.PublicKeyPem,
	)
	if err != nil {
		return nil, nil, err
	}
	if publicKey == nil {
		return nil, nil, errors.New(
			"failed parse PublicKey from string",
		)
	}
	if err := verifier.Verify(publicKey, httpsig.RSA_SHA256); err != nil {
		return nil, nil, err
	}

	givenDigest := request.Header.Get("Digest")
	hash := sha256.New()
	_, _ = hash.Write(body)
	calculatedDigest := "SHA-256=" +
		base64.StdEncoding.EncodeToString(hash.Sum(nil))
	if givenDigest != calculatedDigest {
		return nil, nil, errors.New("digest header is mismatch")
	}

	activity, err := parseInboundActivity(body)
	if err != nil {
		return nil, nil, err
	}
	if err := verifySignatureActorBinding(
		keyID,
		keyOwnerActor,
		activity.Actor,
	); err != nil {
		return nil, nil, err
	}

	remoteActor, err := models.NewActivityPubActorFromRemoteActor(
		activity.Actor,
		relayRemoteUserAgent(),
		ActorCache,
		RemoteRequestSigner,
	)
	if err != nil {
		return nil, nil, err
	}
	if err := verifyActivityActorDocument(
		activity.Actor,
		remoteActor,
	); err != nil {
		return nil, nil, err
	}

	return activity, &remoteActor, nil
}

func normalizedRemoteHost(address string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(
		strings.TrimSuffix(parsed.Hostname(), "."),
	)
	if host == "" {
		return "", errors.New("remote URL has no host")
	}
	return host, nil
}

func verifySignatureActorBinding(
	keyID string,
	keyOwner models.Actor,
	activityActor string,
) error {
	actorHost, err := normalizedRemoteHost(activityActor)
	if err != nil {
		return fmt.Errorf("invalid activity actor: %w", err)
	}
	keyHost, err := normalizedRemoteHost(keyID)
	if err != nil {
		return fmt.Errorf("invalid signature key ID: %w", err)
	}
	if keyHost != actorHost {
		return errors.New(
			"signature key host does not match activity actor host",
		)
	}
	if keyOwner.ID != "" {
		ownerHost, err := normalizedRemoteHost(keyOwner.ID)
		if err != nil || ownerHost != actorHost {
			return errors.New(
				"signature key owner does not match activity actor host",
			)
		}
	}
	if keyOwner.PublicKey.Owner != "" {
		ownerHost, err := normalizedRemoteHost(
			keyOwner.PublicKey.Owner,
		)
		if err != nil || ownerHost != actorHost {
			return errors.New(
				"public key owner does not match activity actor host",
			)
		}
	}
	return nil
}

func verifyActivityActorDocument(
	activityActor string,
	remoteActor models.Actor,
) error {
	activityHost, err := normalizedRemoteHost(activityActor)
	if err != nil {
		return fmt.Errorf("invalid activity actor: %w", err)
	}
	remoteHost, err := normalizedRemoteHost(remoteActor.ID)
	if err != nil || remoteHost != activityHost {
		return errors.New(
			"resolved actor document does not match activity actor host",
		)
	}
	return nil
}

func fetchOriginalActivityFromURL(
	address string,
) (*models.Activity, *models.Actor, error) {
	remoteActivity, err :=
		models.NewActivityPubActivityFromRemoteActivity(
			address,
			relayRemoteUserAgent(),
			RemoteRequestSigner,
		)
	if err != nil {
		return nil, nil, err
	}
	remoteActor, err := models.NewActivityPubActorFromRemoteActor(
		remoteActivity.Actor,
		relayRemoteUserAgent(),
		ActorCache,
		RemoteRequestSigner,
	)
	if err != nil {
		return &remoteActivity, nil, err
	}
	return &remoteActivity, &remoteActor, nil
}
