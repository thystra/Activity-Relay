package directoryclient

import (
	"errors"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	MaximumProfileScalarBytes      = 256
	MaximumProfileNoteBytes        = 1024
	MaximumProfileListItems        = 16
	MaximumProfileListItemBytes    = 128
	MaximumProfileURLBytes         = 2048
	MaximumProfileEmailBytes       = 320
	MaximumProfileFediverseIDBytes = 256
)

var ErrProfile = errors.New("directory profile is invalid")

type RelayProfile struct {
	ParticipationMode string   `json:"participation_mode" yaml:"participation_mode"`
	Availability      string   `json:"availability" yaml:"availability"`
	RelayType         string   `json:"relay_type" yaml:"relay_type"`
	Languages         []string `json:"languages" yaml:"languages"`
	Countries         []string `json:"countries" yaml:"countries"`
	Regions           []string `json:"regions" yaml:"regions"`
	Topics            []string `json:"topics" yaml:"topics"`
	ContactFediverse  string   `json:"contact_fediverse" yaml:"contact_fediverse"`
	ContactEmail      string   `json:"contact_email" yaml:"contact_email"`
	ContactURL        string   `json:"contact_url" yaml:"contact_url"`
	ParticipationURL  string   `json:"participation_url" yaml:"participation_url"`
	Notes             string   `json:"notes" yaml:"notes"`
}

func NormalizeRelayProfile(profile RelayProfile) (RelayProfile, error) {
	var result RelayProfile
	var err error
	if result.ParticipationMode, err = normalizeProfileText(profile.ParticipationMode, MaximumProfileScalarBytes); err != nil {
		return RelayProfile{}, err
	}
	if result.Availability, err = normalizeProfileText(profile.Availability, MaximumProfileScalarBytes); err != nil {
		return RelayProfile{}, err
	}
	if result.RelayType, err = normalizeProfileText(profile.RelayType, MaximumProfileScalarBytes); err != nil {
		return RelayProfile{}, err
	}
	if result.Languages, err = normalizeProfileList(profile.Languages); err != nil {
		return RelayProfile{}, err
	}
	if result.Countries, err = normalizeProfileList(profile.Countries); err != nil {
		return RelayProfile{}, err
	}
	if result.Regions, err = normalizeProfileList(profile.Regions); err != nil {
		return RelayProfile{}, err
	}
	if result.Topics, err = normalizeProfileList(profile.Topics); err != nil {
		return RelayProfile{}, err
	}
	if result.ContactFediverse, err = normalizeProfileText(profile.ContactFediverse, MaximumProfileFediverseIDBytes); err != nil {
		return RelayProfile{}, err
	}
	if result.ContactEmail, err = normalizeProfileEmail(profile.ContactEmail); err != nil {
		return RelayProfile{}, err
	}
	if result.ContactURL, err = normalizeProfileURL(profile.ContactURL); err != nil {
		return RelayProfile{}, err
	}
	if result.ParticipationURL, err = normalizeProfileURL(profile.ParticipationURL); err != nil {
		return RelayProfile{}, err
	}
	if result.Notes, err = normalizeProfileText(profile.Notes, MaximumProfileNoteBytes); err != nil {
		return RelayProfile{}, err
	}
	return result, nil
}

func normalizeProfileText(value string, maximum int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !utf8.ValidString(value) || len(value) > maximum || containsProfileControl(value) {
		return "", ErrProfile
	}
	return value, nil
}

func normalizeProfileList(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > MaximumProfileListItems {
		return nil, ErrProfile
	}
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized, err := normalizeProfileText(value, MaximumProfileListItemBytes)
		if err != nil || normalized == "" {
			return nil, ErrProfile
		}
		unique[normalized] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeProfileEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !utf8.ValidString(value) || len(value) > MaximumProfileEmailBytes || containsProfileControl(value) {
		return "", ErrProfile
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || strings.ContainsAny(value, "<>\"") {
		return "", ErrProfile
	}
	return value, nil
}

// Profile links deliberately accept a conservative subset of the Directory's
// canonical HTTPS URL grammar: canonical authority plus an unescaped absolute
// path. This guarantees every client-accepted link is accepted by the server.
func normalizeProfileURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !utf8.ValidString(value) || len(value) > MaximumProfileURLBytes || containsProfileControl(value) {
		return "", ErrProfile
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.ForceQuery || parsed.Opaque != "" || parsed.String() != value {
		return "", ErrProfile
	}
	if _, err := ParseOrigin("https://" + parsed.Host); err != nil {
		return "", ErrProfile
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	if strings.Contains(path, "%") || strings.Contains(path, "\\") || strings.Contains(path, "//") {
		return "", ErrProfile
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return "", ErrProfile
		}
	}
	return "https://" + parsed.Host + path, nil
}

func containsProfileControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}
