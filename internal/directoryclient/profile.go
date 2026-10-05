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

const (
	ParticipationOpen       = "open"
	ParticipationRestricted = "restricted"
	ParticipationClosed     = "closed"
)

var ErrProfile = errors.New("directory profile is invalid")

type ProfileWarning struct {
	Field   string
	Message string
}

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

func validParticipationMode(value string) bool {
	switch value {
	case "", ParticipationOpen, ParticipationRestricted, ParticipationClosed:
		return true
	default:
		return false
	}
}

func NormalizeRelayProfile(profile RelayProfile) (RelayProfile, error) {
	var result RelayProfile
	var err error
	if result.ParticipationMode, err = normalizeProfileText(profile.ParticipationMode, MaximumProfileScalarBytes); err != nil || !validParticipationMode(result.ParticipationMode) {
		return RelayProfile{}, ErrProfile
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

// SanitizeRelayProfile validates each optional descriptive field independently.
// Invalid metadata is omitted so a profile typo cannot stop the relay or be sent
// to a Directory. Callers should surface every returned warning to the operator.
func SanitizeRelayProfile(profile RelayProfile) (RelayProfile, []ProfileWarning) {
	var result RelayProfile
	warnings := make([]ProfileWarning, 0)
	setText := func(field, value string, maximum int, destination *string) {
		normalized, err := normalizeProfileText(value, maximum)
		if err != nil {
			warnings = append(warnings, ProfileWarning{Field: field, Message: "invalid value; field will be omitted"})
			return
		}
		*destination = normalized
	}
	setList := func(field string, values []string, destination *[]string) {
		normalized, err := normalizeProfileList(values)
		if err != nil {
			warnings = append(warnings, ProfileWarning{Field: field, Message: "invalid list; field will be omitted"})
			return
		}
		*destination = normalized
	}

	participation, err := normalizeProfileText(profile.ParticipationMode, MaximumProfileScalarBytes)
	if err != nil || !validParticipationMode(participation) {
		warnings = append(warnings, ProfileWarning{Field: "participation_mode", Message: "invalid value; allowed values are open, restricted, closed; field will be omitted"})
	} else {
		result.ParticipationMode = participation
	}
	setText("availability", profile.Availability, MaximumProfileScalarBytes, &result.Availability)
	setText("relay_type", profile.RelayType, MaximumProfileScalarBytes, &result.RelayType)
	setList("languages", profile.Languages, &result.Languages)
	setList("countries", profile.Countries, &result.Countries)
	setList("regions", profile.Regions, &result.Regions)
	setList("topics", profile.Topics, &result.Topics)
	setText("contact_fediverse", profile.ContactFediverse, MaximumProfileFediverseIDBytes, &result.ContactFediverse)
	if normalized, err := normalizeProfileEmail(profile.ContactEmail); err != nil {
		warnings = append(warnings, ProfileWarning{Field: "contact_email", Message: "invalid email address; field will be omitted"})
	} else {
		result.ContactEmail = normalized
	}
	if normalized, err := normalizeProfileURL(profile.ContactURL); err != nil {
		warnings = append(warnings, ProfileWarning{Field: "contact_url", Message: "invalid HTTPS URL; field will be omitted"})
	} else {
		result.ContactURL = normalized
	}
	if normalized, err := normalizeProfileURL(profile.ParticipationURL); err != nil {
		warnings = append(warnings, ProfileWarning{Field: "participation_url", Message: "invalid HTTPS URL; field will be omitted"})
	} else {
		result.ParticipationURL = normalized
	}
	setText("notes", profile.Notes, MaximumProfileNoteBytes, &result.Notes)
	return result, warnings
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
