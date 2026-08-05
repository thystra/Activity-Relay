package directoryclient

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const MaximumDirectories = 8

var ErrDirectoryConfiguration = errors.New("directory client configuration is invalid")

// Directory configures one independent directory origin. Lifecycle commands
// refuse it unless Enabled is explicitly true.
type Directory struct {
	Origin  string `mapstructure:"origin" yaml:"origin"`
	Enabled bool   `mapstructure:"enabled" yaml:"enabled"`
}

// ParseDirectories validates a bounded, duplicate-free list. Parsing never
// performs network behavior.
func ParseDirectories(entries []Directory) ([]Directory, error) {
	if len(entries) > MaximumDirectories {
		return nil, ErrDirectoryConfiguration
	}
	parsed := make([]Directory, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for index, entry := range entries {
		origin, err := ParseOrigin(entry.Origin)
		if err != nil {
			return nil, err
		}
		canonical := origin.String()
		if _, duplicate := seen[canonical]; duplicate {
			return nil, ErrDirectoryConfiguration
		}
		seen[canonical] = struct{}{}
		parsed[index] = Directory{Origin: canonical, Enabled: entry.Enabled}
	}
	return parsed, nil
}

// ParseOrigin accepts only one canonical HTTPS origin with no credentials,
// path, query, fragment, default port, or noncanonical case.
func ParseOrigin(value string) (*url.URL, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return nil, ErrDirectoryConfiguration
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
		parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" ||
		parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.ForceQuery || parsed.Opaque != "" ||
		parsed.String() != value {
		return nil, ErrDirectoryConfiguration
	}
	if !canonicalAuthority(parsed) {
		return nil, ErrDirectoryConfiguration
	}
	return parsed, nil
}

func canonicalAuthority(parsed *url.URL) bool {
	hostname := parsed.Hostname()
	port := parsed.Port()
	canonicalHost := hostname
	if address, err := netip.ParseAddr(hostname); err == nil {
		if hostname != address.String() {
			return false
		}
		if address.Is6() {
			canonicalHost = "[" + hostname + "]"
		}
	} else {
		if strings.ToLower(hostname) != hostname || strings.HasSuffix(hostname, ".") ||
			len(hostname) > 253 || !strings.Contains(hostname, ".") {
			return false
		}
		for _, label := range strings.Split(hostname, ".") {
			if !validDNSLabel(label) {
				return false
			}
		}
	}
	if port != "" {
		portNumber, err := strconv.ParseUint(port, 10, 16)
		if err != nil || portNumber == 0 || portNumber == 443 ||
			strconv.FormatUint(portNumber, 10) != port {
			return false
		}
		canonicalHost += ":" + port
	}
	return parsed.Host == canonicalHost
}

func validDNSLabel(label string) bool {
	if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, character := range label {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '-' {
			continue
		}
		return false
	}
	return true
}
