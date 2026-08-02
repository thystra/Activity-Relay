package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/thystra/Activity-Relay/internal/httpsignature"
	"github.com/thystra/Activity-Relay/models"
)

type activityPubRFC9421KeyResolver struct{}

func relayRemoteUserAgent() string {
	return fmt.Sprintf(
		"%s (golang net/http; Activity-Relay %s; %s)",
		GlobalConfig.ServerServiceName(),
		version,
		GlobalConfig.ServerHostname().Host,
	)
}

func (activityPubRFC9421KeyResolver) ResolveRFC9421Key(
	_ context.Context,
	keyID string,
) (httpsignature.RFC9421ResolvedKey, error) {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return httpsignature.RFC9421ResolvedKey{},
			errors.New("RFC 9421 key ID is empty")
	}
	if ActorCache == nil {
		return httpsignature.RFC9421ResolvedKey{},
			errors.New("ActivityPub actor cache is unavailable")
	}
	if RemoteRequestSigner == nil {
		return httpsignature.RFC9421ResolvedKey{},
			errors.New("remote request signer is unavailable")
	}

	actor, err := models.NewActivityPubActorFromRemoteActor(
		keyID,
		relayRemoteUserAgent(),
		ActorCache,
		RemoteRequestSigner,
	)
	if err != nil {
		return httpsignature.RFC9421ResolvedKey{}, err
	}
	if strings.TrimSpace(actor.ID) == "" {
		return httpsignature.RFC9421ResolvedKey{},
			errors.New("resolved ActivityPub actor has no ID")
	}
	if strings.TrimSpace(actor.PublicKey.ID) == "" {
		return httpsignature.RFC9421ResolvedKey{},
			errors.New("resolved ActivityPub actor has no public-key ID")
	}
	if actor.PublicKey.ID != keyID {
		return httpsignature.RFC9421ResolvedKey{},
			errors.New("resolved ActivityPub public-key ID does not match keyid")
	}
	if strings.TrimSpace(actor.PublicKey.Owner) == "" {
		return httpsignature.RFC9421ResolvedKey{},
			errors.New("resolved ActivityPub public key has no owner")
	}

	publicKey, err := models.ReadPublicKeyRSAFromString(
		actor.PublicKey.PublicKeyPem,
	)
	if err != nil {
		return httpsignature.RFC9421ResolvedKey{},
			fmt.Errorf("parse resolved ActivityPub RSA public key: %w", err)
	}
	if publicKey == nil {
		return httpsignature.RFC9421ResolvedKey{},
			errors.New("resolved ActivityPub RSA public key is nil")
	}

	return httpsignature.RFC9421ResolvedKey{
		KeyID:     actor.PublicKey.ID,
		Owner:     actor.PublicKey.Owner,
		ActorID:   actor.ID,
		PublicKey: publicKey,
	}, nil
}

func newInboundRFC9421Verifier(
	globalConfig *models.RelayConfig,
) (*httpsignature.RFC9421Verifier, error) {
	if globalConfig == nil || globalConfig.ServerHostname() == nil {
		return nil, errors.New(
			"RFC 9421 public server identity is unavailable",
		)
	}

	nonceStore, err := httpsignature.NewRedisRFC9421NonceStore(
		globalConfig.RedisClient(),
		"",
	)
	if err != nil {
		return nil, err
	}

	return httpsignature.NewRFC9421Verifier(
		httpsignature.RFC9421VerifierOptions{
			Scheme:      globalConfig.ServerHostname().Scheme,
			Authority:   globalConfig.ServerHostname().Host,
			KeyResolver: activityPubRFC9421KeyResolver{},
			NonceStore:  nonceStore,
		},
	)
}
