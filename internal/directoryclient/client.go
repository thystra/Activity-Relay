package directoryclient

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	registerPath          = "/v1/relays/register"
	heartbeatPath         = "/v1/relays/heartbeat"
	unregisterPath        = "/v1/relays/unregister"
	statusPath            = "/v1/status"
	maximumResponseBytes  = int64(16 * 1024)
	maximumErrorMessage   = 256
	defaultRequestTimeout = 15 * time.Second
	MaximumRetryAfter     = 30 * time.Second
)

var (
	ErrDirectoryResponse  = errors.New("directory response is invalid")
	ErrResponseTooLarge   = errors.New("directory response is too large")
	ErrDirectoryTransport = errors.New("directory transport failed")
)

// ProtocolError is a validated, closed-vocabulary remote result. The remote
// human message and raw response body are deliberately not retained.
type ProtocolError struct {
	StatusCode int
	Code       ErrorCode
	RetryAfter time.Duration
}

// Status is the validated public Directory status document.
type Status struct {
	SchemaVersion      int    `json:"schema_version"`
	Service            string `json:"service"`
	Version            string `json:"version"`
	PublicBaseURL      string `json:"public_base_url"`
	LifecycleEnabled   bool   `json:"lifecycle_enabled"`
	LifecycleAvailable bool   `json:"lifecycle_available"`
	EnrollmentOpen     bool   `json:"enrollment_open"`
}

func (err *ProtocolError) Error() string {
	if err == nil {
		return "directory protocol error"
	}
	return fmt.Sprintf("directory request failed with %s", err.Code)
}

// Options configures one independently enabled directory client.
type Options struct {
	Origin        string
	RelayActor    string
	PublicBaseURL string
	KeyID         string
	PrivateKey    *rsa.PrivateKey
	HTTPClient    *http.Client
	Now           func() time.Time
	Nonce         func() (string, error)
}

// Client signs strict version 1 lifecycle requests. Construction has no
// network side effects and does not activate scheduling.
type Client struct {
	origin        *url.URL
	relayActor    string
	publicBaseURL string
	signer        *requestSigner
	httpClient    *http.Client
}

func New(options Options) (*Client, error) {
	origin, err := ParseOrigin(options.Origin)
	if err != nil || !validRelayIdentity(options.RelayActor, options.PublicBaseURL) {
		return nil, ErrDirectoryConfiguration
	}
	signer, err := newRequestSigner(options.KeyID, options.PrivateKey, options.Now, options.Nonce)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{}
	if options.HTTPClient != nil {
		*httpClient = *options.HTTPClient
	}
	if httpClient.Timeout <= 0 || httpClient.Timeout > defaultRequestTimeout {
		httpClient.Timeout = defaultRequestTimeout
	}
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{
		origin:        origin,
		relayActor:    options.RelayActor,
		publicBaseURL: options.PublicBaseURL,
		signer:        signer,
		httpClient:    httpClient,
	}, nil
}

func validRelayIdentity(actor, publicBase string) bool {
	actorURL, actorErr := url.ParseRequestURI(actor)
	baseURL, baseErr := ParseOrigin(publicBase)
	if actorErr != nil || baseErr != nil || actorURL.Scheme != "https" ||
		baseURL.Scheme != "https" || actorURL.User != nil || baseURL.User != nil ||
		actorURL.RawQuery != "" || actorURL.Fragment != "" || actorURL.ForceQuery ||
		strings.Contains(actor, "#") ||
		baseURL.RawQuery != "" || baseURL.Fragment != "" || baseURL.ForceQuery ||
		actorURL.Host == "" || baseURL.Host == "" || actorURL.Host != baseURL.Host ||
		baseURL.Path != "" || actorURL.Path == "" ||
		actorURL.String() != actor || baseURL.String() != publicBase {
		return false
	}
	if strings.Contains(actorURL.EscapedPath(), "//") {
		return false
	}
	for _, segment := range strings.Split(actorURL.EscapedPath(), "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func (client *Client) Register(ctx context.Context) (Response, error) {
	return client.send(ctx, OperationRegister, registerRequest{
		ProtocolVersion: ProtocolVersion,
		Operation:       OperationRegister,
		RelayActor:      client.relayActor,
		PublicBaseURL:   client.publicBaseURL,
	})
}

func (client *Client) Heartbeat(ctx context.Context) (Response, error) {
	return client.send(ctx, OperationHeartbeat, identityRequest{
		ProtocolVersion: ProtocolVersion,
		Operation:       OperationHeartbeat,
		RelayActor:      client.relayActor,
	})
}

func (client *Client) Unregister(ctx context.Context) (Response, error) {
	return client.send(ctx, OperationUnregister, identityRequest{
		ProtocolVersion: ProtocolVersion,
		Operation:       OperationUnregister,
		RelayActor:      client.relayActor,
	})
}

// Status retrieves the unsigned public status document using the same bounded,
// redirect-refusing transport and strict response decoder as lifecycle calls.
func (client *Client) Status(ctx context.Context) (Status, error) {
	if client == nil || client.origin == nil || client.httpClient == nil || ctx == nil {
		return Status{}, ErrDirectoryConfiguration
	}
	target := *client.origin
	target.Path = statusPath
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return Status{}, ErrDirectoryConfiguration
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return Status{}, ErrDirectoryTransport
	}
	defer response.Body.Close()
	body, err := readResponseBody(response.Body)
	if err != nil {
		return Status{}, err
	}
	if !validJSONMediaType(response.Header.Get("Content-Type")) {
		return Status{}, ErrDirectoryResponse
	}
	if response.StatusCode != http.StatusOK {
		return Status{}, decodeProtocolError(response.StatusCode, response.Header, body)
	}
	var status Status
	if err := decodeStrictJSON(body, &status); err != nil || status.SchemaVersion != 2 ||
		status.Service != "activity-relay-directory" || status.Version == "" ||
		status.PublicBaseURL != client.origin.String() {
		return Status{}, ErrDirectoryResponse
	}
	return status, nil
}

// HeartbeatWithRegisterReconciliation performs one register reconciliation
// only for the explicit relay_not_registered result, then makes one final
// heartbeat attempt. No other error class triggers registration.
func (client *Client) HeartbeatWithRegisterReconciliation(ctx context.Context) (Response, error) {
	response, err := client.Heartbeat(ctx)
	if err == nil {
		return response, nil
	}
	var protocolError *ProtocolError
	if !errors.As(err, &protocolError) || protocolError.Code != ErrorRelayNotRegistered {
		return Response{}, err
	}
	if _, err := client.Register(ctx); err != nil {
		return Response{}, err
	}
	return client.Heartbeat(ctx)
}

func (client *Client) send(
	ctx context.Context,
	operation Operation,
	document any,
) (Response, error) {
	if client == nil || client.origin == nil || client.signer == nil ||
		client.httpClient == nil || ctx == nil || !operation.valid() {
		return Response{}, ErrDirectoryConfiguration
	}
	body, err := json.Marshal(document)
	if err != nil {
		return Response{}, ErrDirectoryConfiguration
	}
	target := *client.origin
	target.Path = operationPath(operation)
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		target.String(),
		bytes.NewReader(body),
	)
	if err != nil {
		return Response{}, ErrDirectoryConfiguration
	}
	if err := client.signer.sign(request, body); err != nil {
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
		return decodeSuccess(response.StatusCode, operation, client.relayActor, responseBody)
	}
	return Response{}, decodeProtocolError(response.StatusCode, response.Header, responseBody)
}

func operationPath(operation Operation) string {
	switch operation {
	case OperationRegister:
		return registerPath
	case OperationHeartbeat:
		return heartbeatPath
	case OperationUnregister:
		return unregisterPath
	default:
		return ""
	}
}

func readResponseBody(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, ErrDirectoryResponse
	}
	body, err := io.ReadAll(io.LimitReader(reader, maximumResponseBytes+1))
	if err != nil {
		return nil, ErrDirectoryResponse
	}
	if int64(len(body)) > maximumResponseBytes {
		return nil, ErrResponseTooLarge
	}
	return body, nil
}

func validJSONMediaType(value string) bool {
	mediaType, parameters, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json" && len(parameters) == 0
}

func decodeSuccess(
	status int,
	operation Operation,
	relayActor string,
	body []byte,
) (Response, error) {
	if (operation == OperationRegister && status != http.StatusOK && status != http.StatusCreated) ||
		(operation != OperationRegister && status != http.StatusOK) {
		return Response{}, ErrDirectoryResponse
	}
	var response Response
	if err := decodeStrictJSON(body, &response); err != nil ||
		response.ProtocolVersion != ProtocolVersion ||
		response.Operation != operation || !response.Outcome.validFor(operation) ||
		response.RelayActor != relayActor {
		return Response{}, ErrDirectoryResponse
	}
	return response, nil
}

func decodeProtocolError(status int, header http.Header, body []byte) error {
	var response errorResponse
	if err := decodeStrictJSON(body, &response); err != nil ||
		response.ProtocolVersion != ProtocolVersion || !response.Error.Code.valid() ||
		response.Error.Message == "" || len(response.Error.Message) > maximumErrorMessage ||
		!errorStatusMatches(response.Error.Code, status) {
		return ErrDirectoryResponse
	}
	retryAfter, err := boundedRetryAfter(response.Error.Code, header)
	if err != nil {
		return ErrDirectoryResponse
	}
	return &ProtocolError{
		StatusCode: status,
		Code:       response.Error.Code,
		RetryAfter: retryAfter,
	}
}

func boundedRetryAfter(code ErrorCode, header http.Header) (time.Duration, error) {
	if code != ErrorRateLimited {
		return 0, nil
	}
	value := header.Get("Retry-After")
	if value == "" {
		return 0, nil
	}
	seconds, err := strconv.ParseUint(value, 10, 31)
	if err != nil || seconds == 0 {
		return 0, ErrDirectoryResponse
	}
	delay := time.Duration(seconds) * time.Second
	if delay > MaximumRetryAfter {
		delay = MaximumRetryAfter
	}
	return delay, nil
}

func errorStatusMatches(code ErrorCode, status int) bool {
	switch code {
	case ErrorInvalidRequest:
		return status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge
	case ErrorUnsupportedProtocolVersion:
		return status == http.StatusBadRequest
	case ErrorAuthenticationFailed:
		return status == http.StatusUnauthorized
	case ErrorReplayDetected, ErrorRelayNotRegistered:
		return status == http.StatusConflict
	case ErrorLifecycleUnavailable:
		return status == http.StatusServiceUnavailable
	case ErrorEnrollmentClosed, ErrorRelaySuspended:
		return status == http.StatusForbidden
	case ErrorRateLimited:
		return status == http.StatusTooManyRequests
	case ErrorInternal:
		return status == http.StatusInternalServerError || status == http.StatusServiceUnavailable
	default:
		return false
	}
}

func decodeStrictJSON(body []byte, destination any) error {
	if len(body) == 0 || !json.Valid(body) || duplicateJSONName(body) {
		return ErrDirectoryResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return ErrDirectoryResponse
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrDirectoryResponse
	}
	return nil
}

func duplicateJSONName(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var scan func() bool
	scan = func() bool {
		token, err := decoder.Token()
		if err != nil {
			return true
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return false
		}
		switch delimiter {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				nameToken, err := decoder.Token()
				name, ok := nameToken.(string)
				if err != nil || !ok {
					return true
				}
				if _, exists := seen[name]; exists {
					return true
				}
				seen[name] = struct{}{}
				if scan() {
					return true
				}
			}
			end, err := decoder.Token()
			return err != nil || end != json.Delim('}')
		case '[':
			for decoder.More() {
				if scan() {
					return true
				}
			}
			end, err := decoder.Token()
			return err != nil || end != json.Delim(']')
		default:
			return true
		}
	}
	if scan() {
		return true
	}
	_, err := decoder.Token()
	return !errors.Is(err, io.EOF)
}
