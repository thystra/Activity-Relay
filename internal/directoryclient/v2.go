package directoryclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

const ProtocolVersion2 = 2

const (
	registerPathV2   = "/v2/relays/register"
	heartbeatPathV2  = "/v2/relays/heartbeat"
	unregisterPathV2 = "/v2/relays/unregister"
)

type v2RegisterRequest struct {
	ProtocolVersion int           `json:"protocol_version"`
	Operation       Operation     `json:"operation"`
	RelayActor      string        `json:"relay_actor"`
	PublicBaseURL   string        `json:"public_base_url"`
	Profile         v2ProfileWire `json:"profile"`
}

type v2ProfileWire struct {
	ParticipationMode string   `json:"participation_mode"`
	Availability      string   `json:"availability"`
	RelayType         string   `json:"relay_type"`
	Languages         []string `json:"languages"`
	Countries         []string `json:"countries"`
	Regions           []string `json:"regions"`
	Topics            []string `json:"topics"`
	ContactFediverse  string   `json:"contact_fediverse"`
	ContactEmail      string   `json:"contact_email"`
	ContactURL        string   `json:"contact_url"`
	ParticipationURL  string   `json:"participation_url"`
	Notes             string   `json:"notes"`
}

func profileWire(profile RelayProfile) v2ProfileWire {
	copyList := func(values []string) []string {
		if len(values) == 0 {
			return []string{}
		}
		return append([]string(nil), values...)
	}
	return v2ProfileWire{
		ParticipationMode: profile.ParticipationMode, Availability: profile.Availability,
		RelayType: profile.RelayType, Languages: copyList(profile.Languages),
		Countries: copyList(profile.Countries), Regions: copyList(profile.Regions),
		Topics: copyList(profile.Topics), ContactFediverse: profile.ContactFediverse,
		ContactEmail: profile.ContactEmail, ContactURL: profile.ContactURL,
		ParticipationURL: profile.ParticipationURL, Notes: profile.Notes,
	}
}

func (client *Client) ProfileDigest() string {
	if client == nil {
		return ""
	}
	body, err := json.Marshal(profileWire(client.profile))
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func validStatusLifecycleVersions(status Status) bool {
	switch status.SchemaVersion {
	case 2, 3:
		return len(status.LifecycleProtocolVersions) == 0
	case 4:
		if len(status.LifecycleProtocolVersions) == 0 {
			return false
		}
		previous := 0
		hasV1 := false
		for _, version := range status.LifecycleProtocolVersions {
			if version <= previous {
				return false
			}
			previous = version
			if version == ProtocolVersion {
				hasV1 = true
			}
		}
		return hasV1
	default:
		return false
	}
}

func (client *Client) negotiatedProtocolVersion(ctx context.Context) (int, error) {
	status, err := client.Status(ctx)
	if err != nil {
		return 0, err
	}
	if status.SchemaVersion >= 4 {
		for index := len(status.LifecycleProtocolVersions) - 1; index >= 0; index-- {
			switch status.LifecycleProtocolVersions[index] {
			case ProtocolVersion2:
				return ProtocolVersion2, nil
			case ProtocolVersion:
				return ProtocolVersion, nil
			}
		}
	}
	return ProtocolVersion, nil
}

func (client *Client) RegisterNegotiated(ctx context.Context) (Response, error) {
	version, err := client.negotiatedProtocolVersion(ctx)
	if err != nil {
		return Response{}, err
	}
	return client.registerVersion(ctx, version)
}

func (client *Client) HeartbeatNegotiated(ctx context.Context) (Response, error) {
	version, err := client.negotiatedProtocolVersion(ctx)
	if err != nil {
		return Response{}, err
	}
	return client.heartbeatVersion(ctx, version)
}

func (client *Client) UnregisterNegotiated(ctx context.Context) (Response, error) {
	version, err := client.negotiatedProtocolVersion(ctx)
	if err != nil {
		return Response{}, err
	}
	return client.unregisterVersion(ctx, version)
}

func (client *Client) HeartbeatWithRegisterReconciliationNegotiated(ctx context.Context) (Response, error) {
	version, err := client.negotiatedProtocolVersion(ctx)
	if err != nil {
		return Response{}, err
	}
	response, err := client.heartbeatVersion(ctx, version)
	if err == nil {
		return response, nil
	}
	var protocolError *ProtocolError
	if !errors.As(err, &protocolError) || protocolError.Code != ErrorRelayNotRegistered {
		return Response{}, err
	}
	if _, err := client.registerVersion(ctx, version); err != nil {
		return Response{}, err
	}
	return client.heartbeatVersion(ctx, version)
}

func (client *Client) registerVersion(ctx context.Context, version int) (Response, error) {
	if version == ProtocolVersion {
		return client.send(ctx, OperationRegister, registerRequest{ProtocolVersion: ProtocolVersion, Operation: OperationRegister, RelayActor: client.relayActor, PublicBaseURL: client.publicBaseURL})
	}
	if version != ProtocolVersion2 {
		return Response{}, ErrDirectoryConfiguration
	}
	return client.sendV2(ctx, OperationRegister, v2RegisterRequest{ProtocolVersion: ProtocolVersion2, Operation: OperationRegister, RelayActor: client.relayActor, PublicBaseURL: client.publicBaseURL, Profile: profileWire(client.profile)})
}

func (client *Client) heartbeatVersion(ctx context.Context, version int) (Response, error) {
	if version == ProtocolVersion {
		return client.send(ctx, OperationHeartbeat, identityRequest{ProtocolVersion: ProtocolVersion, Operation: OperationHeartbeat, RelayActor: client.relayActor})
	}
	if version != ProtocolVersion2 {
		return Response{}, ErrDirectoryConfiguration
	}
	return client.sendV2(ctx, OperationHeartbeat, identityRequest{ProtocolVersion: ProtocolVersion2, Operation: OperationHeartbeat, RelayActor: client.relayActor})
}

func (client *Client) unregisterVersion(ctx context.Context, version int) (Response, error) {
	if version == ProtocolVersion {
		return client.send(ctx, OperationUnregister, identityRequest{ProtocolVersion: ProtocolVersion, Operation: OperationUnregister, RelayActor: client.relayActor})
	}
	if version != ProtocolVersion2 {
		return Response{}, ErrDirectoryConfiguration
	}
	return client.sendV2(ctx, OperationUnregister, identityRequest{ProtocolVersion: ProtocolVersion2, Operation: OperationUnregister, RelayActor: client.relayActor})
}

func (client *Client) sendV2(ctx context.Context, operation Operation, document any) (Response, error) {
	if client == nil || client.origin == nil || client.signer == nil || client.httpClient == nil || ctx == nil || !operation.valid() {
		return Response{}, ErrDirectoryConfiguration
	}
	body, err := json.Marshal(document)
	if err != nil {
		return Response{}, ErrDirectoryConfiguration
	}
	target := *client.origin
	switch operation {
	case OperationRegister:
		target.Path = registerPathV2
	case OperationHeartbeat:
		target.Path = heartbeatPathV2
	case OperationUnregister:
		target.Path = unregisterPathV2
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return Response{}, ErrDirectoryConfiguration
	}
	if err := client.signer.signWithTag(request, body, SignatureTagV2); err != nil {
		return Response{}, err
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return Response{}, ErrDirectoryTransport
	}
	defer response.Body.Close()
	responseBody, err := readResponseBody(response.Body)
	if err != nil {
		return Response{}, err
	}
	if !validJSONMediaType(response.Header.Get("Content-Type")) {
		return Response{}, ErrDirectoryResponse
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return decodeSuccessVersion(response.StatusCode, operation, client.relayActor, ProtocolVersion2, responseBody)
	}
	return Response{}, decodeProtocolErrorAtVersion(response.StatusCode, response.Header, ProtocolVersion2, responseBody, client.now().UTC())
}

func decodeSuccessVersion(status int, operation Operation, relayActor string, protocolVersion int, body []byte) (Response, error) {
	if (operation == OperationRegister && status != http.StatusOK && status != http.StatusCreated) || (operation != OperationRegister && status != http.StatusOK) {
		return Response{}, ErrDirectoryResponse
	}
	var response Response
	if err := decodeStrictJSON(body, &response); err != nil || response.ProtocolVersion != protocolVersion || response.Operation != operation || !response.Outcome.validFor(operation) || response.RelayActor != relayActor {
		return Response{}, ErrDirectoryResponse
	}
	return response, nil
}

func decodeProtocolErrorAtVersion(status int, header http.Header, protocolVersion int, body []byte, now time.Time) error {
	var response errorResponse
	if err := decodeStrictJSON(body, &response); err != nil || response.ProtocolVersion != protocolVersion || !response.Error.Code.valid() || response.Error.Message == "" || len(response.Error.Message) > maximumErrorMessage || !errorStatusMatches(response.Error.Code, status) {
		return ErrDirectoryResponse
	}
	retryAfter, err := boundedRetryAfter(response.Error.Code, status, header, now)
	if err != nil {
		return ErrDirectoryResponse
	}
	return &ProtocolError{StatusCode: status, Code: response.Error.Code, RetryAfter: retryAfter}
}
