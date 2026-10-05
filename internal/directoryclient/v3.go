package directoryclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

const ProtocolVersion3 = 3
const MaximumParticipatingInstanceCount = 10_000_000

const (
	registerPathV3   = "/v3/relays/register"
	heartbeatPathV3  = "/v3/relays/heartbeat"
	unregisterPathV3 = "/v3/relays/unregister"
)

type Telemetry struct {
	ParticipatingInstanceCount int
}

type TelemetryProvider func(context.Context) (Telemetry, error)

type v3TelemetryWire struct {
	ParticipatingInstanceCount int `json:"participating_instance_count"`
}

type v3RegisterRequest struct {
	ProtocolVersion int              `json:"protocol_version"`
	Operation       Operation        `json:"operation"`
	RelayActor      string           `json:"relay_actor"`
	PublicBaseURL   string           `json:"public_base_url"`
	Profile         v2ProfileWire    `json:"profile"`
	Telemetry       *v3TelemetryWire `json:"telemetry,omitempty"`
}

type v3HeartbeatRequest struct {
	ProtocolVersion int              `json:"protocol_version"`
	Operation       Operation        `json:"operation"`
	RelayActor      string           `json:"relay_actor"`
	Telemetry       *v3TelemetryWire `json:"telemetry,omitempty"`
}

func (client *Client) currentV3Telemetry(ctx context.Context) *v3TelemetryWire {
	if client == nil || client.telemetry == nil || ctx == nil {
		return nil
	}
	telemetry, err := client.telemetry(ctx)
	if err != nil || telemetry.ParticipatingInstanceCount < 0 || telemetry.ParticipatingInstanceCount > MaximumParticipatingInstanceCount {
		return nil
	}
	return &v3TelemetryWire{ParticipatingInstanceCount: telemetry.ParticipatingInstanceCount}
}

func (client *Client) sendV3(ctx context.Context, operation Operation, document any) (Response, error) {
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
		target.Path = registerPathV3
	case OperationHeartbeat:
		target.Path = heartbeatPathV3
	case OperationUnregister:
		target.Path = unregisterPathV3
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return Response{}, ErrDirectoryConfiguration
	}
	if err := client.signer.signWithTag(request, body, SignatureTagV3); err != nil {
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
		return decodeSuccessVersion(response.StatusCode, operation, client.relayActor, ProtocolVersion3, responseBody)
	}
	return Response{}, decodeProtocolErrorAtVersion(response.StatusCode, response.Header, ProtocolVersion3, responseBody, client.now().UTC())
}
