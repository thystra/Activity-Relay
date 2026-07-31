// File: internal/deliverymeta/metadata.go
package deliverymeta

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
)

const maxIdentifierBytes = 2048

type Metadata struct {
	ActivityID   string
	ActivityType string
	ObjectID     string
	ActorID      string
	OriginDomain string
	BodySHA256   string
}

func FromBody(body []byte) Metadata {
	sum := sha256.Sum256(body)
	metadata := Metadata{BodySHA256: hex.EncodeToString(sum[:])}

	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return metadata
	}

	metadata.ActivityID = bounded(identifier(payload["id"]))
	metadata.ActivityType = bounded(textValue(payload["type"]))
	metadata.ObjectID = bounded(identifier(payload["object"]))
	metadata.ActorID = bounded(identifier(payload["actor"]))
	metadata.OriginDomain = domain(metadata.ActorID)
	return metadata
}

func identifier(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		return identifier(typed["id"])
	case []any:
		for _, entry := range typed {
			if found := identifier(entry); found != "" {
				return found
			}
		}
	}
	return ""
}

func textValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		for _, entry := range typed {
			if found := textValue(entry); found != "" {
				return found
			}
		}
	}
	return ""
}

func domain(identifier string) string {
	parsed, err := url.Parse(identifier)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
}

func bounded(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxIdentifierBytes {
		return value
	}
	return value[:maxIdentifierBytes]
}

// EOF: internal/deliverymeta/metadata.go
