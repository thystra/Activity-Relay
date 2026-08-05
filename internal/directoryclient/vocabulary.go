// Package directoryclient implements the opt-in Activity-Relay Directory
// version 1 client contract. Only explicit commands use it; scheduling is absent.
package directoryclient

const ProtocolVersion = 1

type Operation string

const (
	OperationRegister   Operation = "register"
	OperationHeartbeat  Operation = "heartbeat"
	OperationUnregister Operation = "unregister"
)

func (operation Operation) valid() bool {
	switch operation {
	case OperationRegister, OperationHeartbeat, OperationUnregister:
		return true
	default:
		return false
	}
}

type Outcome string

const (
	OutcomeCreated   Outcome = "created"
	OutcomeUpdated   Outcome = "updated"
	OutcomeUnchanged Outcome = "unchanged"
	OutcomeRecorded  Outcome = "recorded"
	OutcomeRemoved   Outcome = "removed"
	OutcomeAbsent    Outcome = "absent"
)

func (outcome Outcome) validFor(operation Operation) bool {
	switch operation {
	case OperationRegister:
		return outcome == OutcomeCreated || outcome == OutcomeUpdated || outcome == OutcomeUnchanged
	case OperationHeartbeat:
		return outcome == OutcomeRecorded
	case OperationUnregister:
		return outcome == OutcomeRemoved || outcome == OutcomeAbsent
	default:
		return false
	}
}

type ErrorCode string

const (
	ErrorInvalidRequest             ErrorCode = "invalid_request"
	ErrorUnsupportedProtocolVersion ErrorCode = "unsupported_protocol_version"
	ErrorAuthenticationFailed       ErrorCode = "authentication_failed"
	ErrorReplayDetected             ErrorCode = "replay_detected"
	ErrorLifecycleUnavailable       ErrorCode = "lifecycle_unavailable"
	ErrorEnrollmentClosed           ErrorCode = "enrollment_closed"
	ErrorRelayNotRegistered         ErrorCode = "relay_not_registered"
	ErrorRelaySuspended             ErrorCode = "relay_suspended"
	ErrorRateLimited                ErrorCode = "rate_limited"
	ErrorInternal                   ErrorCode = "internal_error"
)

func (code ErrorCode) valid() bool {
	switch code {
	case ErrorInvalidRequest,
		ErrorUnsupportedProtocolVersion,
		ErrorAuthenticationFailed,
		ErrorReplayDetected,
		ErrorLifecycleUnavailable,
		ErrorEnrollmentClosed,
		ErrorRelayNotRegistered,
		ErrorRelaySuspended,
		ErrorRateLimited,
		ErrorInternal:
		return true
	default:
		return false
	}
}

type registerRequest struct {
	ProtocolVersion int       `json:"protocol_version"`
	Operation       Operation `json:"operation"`
	RelayActor      string    `json:"relay_actor"`
	PublicBaseURL   string    `json:"public_base_url"`
}

type identityRequest struct {
	ProtocolVersion int       `json:"protocol_version"`
	Operation       Operation `json:"operation"`
	RelayActor      string    `json:"relay_actor"`
}

// Response is one validated successful lifecycle response.
type Response struct {
	ProtocolVersion int       `json:"protocol_version"`
	Operation       Operation `json:"operation"`
	Outcome         Outcome   `json:"outcome"`
	RelayActor      string    `json:"relay_actor"`
}

type errorResponse struct {
	ProtocolVersion int           `json:"protocol_version"`
	Error           errorDocument `json:"error"`
}

type errorDocument struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}
