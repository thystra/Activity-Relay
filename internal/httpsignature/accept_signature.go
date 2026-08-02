package httpsignature

import (
	"net/http"
	"strings"

	"github.com/dunglas/httpsfv"
)

const rfc9421RSAAlgorithm = "rsa-v1_5-sha256"

func expectedAcceptSignatureComponents(
	scope DestinationScope,
) ([]string, bool) {
	switch scope {
	case DestinationScopeFetch:
		return rfc9421GETComponents, true
	case DestinationScopeDelivery:
		return rfc9421POSTComponents, true
	default:
		return nil, false
	}
}

// CompatibleAcceptSignature reports whether a response asks for exactly the
// RFC 9421 request shape Activity-Relay can generate. Malformed or unsupported
// requests are ignored as evidence rather than failing the completed request.
func CompatibleAcceptSignature(
	values []string,
	scope DestinationScope,
	keyID string,
) bool {
	if len(values) == 0 || strings.TrimSpace(keyID) == "" {
		return false
	}
	expected, ok := expectedAcceptSignatureComponents(scope)
	if !ok {
		return false
	}

	dictionary, err := httpsfv.UnmarshalDictionary(values)
	if err != nil {
		return false
	}
	names := dictionary.Names()
	if len(names) != 1 || names[0] != rfc9421SignatureLabel {
		return false
	}
	member, ok := dictionary.Get(rfc9421SignatureLabel)
	if !ok {
		return false
	}
	inner, ok := member.(httpsfv.InnerList)
	if !ok || len(inner.Items) != len(expected) {
		return false
	}
	for index, item := range inner.Items {
		component, ok := item.Value.(string)
		if !ok || component != expected[index] {
			return false
		}
		if item.Params != nil && len(item.Params.Names()) != 0 {
			return false
		}
	}

	if inner.Params == nil {
		return true
	}
	for _, name := range inner.Params.Names() {
		value, present := inner.Params.Get(name)
		if !present {
			return false
		}
		switch name {
		case "created":
			requested, ok := value.(bool)
			if !ok || !requested {
				return false
			}
		case "alg":
			algorithm, ok := value.(string)
			if !ok || algorithm != rfc9421RSAAlgorithm {
				return false
			}
		case "keyid":
			requestedKeyID, ok := value.(string)
			if !ok || requestedKeyID != keyID {
				return false
			}
		case "tag":
			tag, ok := value.(string)
			if !ok || tag != rfc9421SignatureTag {
				return false
			}
		case "expires", "nonce":
			return false
		default:
			return false
		}
	}
	return true
}

func splitAuthenticationChallenges(value string) []string {
	var challenges []string
	start := 0
	quoted := false
	escaped := false
	for index, character := range value {
		if escaped {
			escaped = false
			continue
		}
		if quoted && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			continue
		}
		if character == ',' && !quoted {
			challenges = append(challenges, value[start:index])
			start = index + 1
		}
	}
	challenges = append(challenges, value[start:])
	return challenges
}

// ExplicitLegacySignatureRejection recognizes only an authentication response
// that explicitly advertises the legacy Signature auth scheme. Generic status
// codes, response bodies, and transport failures are not negotiation evidence.
func ExplicitLegacySignatureRejection(
	statusCode int,
	header http.Header,
) bool {
	if statusCode != http.StatusUnauthorized &&
		statusCode != http.StatusForbidden {
		return false
	}
	for _, value := range header.Values("WWW-Authenticate") {
		for _, challenge := range splitAuthenticationChallenges(value) {
			fields := strings.Fields(strings.TrimSpace(challenge))
			if len(fields) > 0 &&
				strings.EqualFold(fields[0], "Signature") {
				return true
			}
		}
	}
	return false
}
