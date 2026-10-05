package directoryconfig

import (
	"fmt"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"gopkg.in/yaml.v3"
)

var profileKeys = map[string]struct{}{
	"participation_mode": {}, "availability": {}, "relay_type": {},
	"languages": {}, "countries": {}, "regions": {}, "topics": {},
	"contact_fediverse": {}, "contact_email": {}, "contact_url": {},
	"participation_url": {}, "notes": {},
}

type Warning struct {
	Line    int
	Field   string
	Message string
}

func (warning Warning) String() string {
	prefix := "DIRECTORY_PROFILE"
	if warning.Field != "" {
		prefix += "." + warning.Field
	}
	if warning.Line > 0 {
		return fmt.Sprintf("line %d: %s: %s", warning.Line, prefix, warning.Message)
	}
	return fmt.Sprintf("%s: %s", prefix, warning.Message)
}

func profileSetting(root *yaml.Node) (directoryclient.RelayProfile, []Warning, error) {
	node := mappingValue(root, "DIRECTORY_PROFILE")
	if node == nil {
		return directoryclient.RelayProfile{}, nil, nil
	}
	return decodeProfileNode(node)
}

func parseEnvironmentProfile(value string) (directoryclient.RelayProfile, []Warning, error) {
	if value == "" {
		return directoryclient.RelayProfile{}, nil, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(value), &document); err != nil || len(document.Content) != 1 {
		return directoryclient.RelayProfile{}, nil, ErrConfiguration
	}
	if err := validateNode(document.Content[0]); err != nil {
		return directoryclient.RelayProfile{}, nil, err
	}
	return decodeProfileNode(document.Content[0])
}

func decodeProfileNode(node *yaml.Node) (directoryclient.RelayProfile, []Warning, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return directoryclient.RelayProfile{}, nil, ErrConfiguration
	}
	var raw directoryclient.RelayProfile
	warnings := make([]Warning, 0)
	lineByField := make(map[string]int)

	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index]
		value := node.Content[index+1]
		field := key.Value
		lineByField[field] = value.Line
		if _, ok := profileKeys[field]; !ok {
			warnings = append(warnings, Warning{Line: key.Line, Field: field, Message: "unknown optional profile key; field will be ignored"})
			continue
		}
		switch field {
		case "languages", "countries", "regions", "topics":
			values, ok := decodeProfileStringList(value)
			if !ok {
				warnings = append(warnings, Warning{Line: key.Line, Field: field, Message: "must be a YAML list of strings; field will be omitted"})
				continue
			}
			switch field {
			case "languages":
				raw.Languages = values
			case "countries":
				raw.Countries = values
			case "regions":
				raw.Regions = values
			case "topics":
				raw.Topics = values
			}
		default:
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				warnings = append(warnings, Warning{Line: key.Line, Field: field, Message: "must be a string; field will be omitted"})
				continue
			}
			switch field {
			case "participation_mode":
				raw.ParticipationMode = value.Value
			case "availability":
				raw.Availability = value.Value
			case "relay_type":
				raw.RelayType = value.Value
			case "contact_fediverse":
				raw.ContactFediverse = value.Value
			case "contact_email":
				raw.ContactEmail = value.Value
			case "contact_url":
				raw.ContactURL = value.Value
			case "participation_url":
				raw.ParticipationURL = value.Value
			case "notes":
				raw.Notes = value.Value
			}
		}
	}

	normalized, profileWarnings := directoryclient.SanitizeRelayProfile(raw)
	for _, profileWarning := range profileWarnings {
		warnings = append(warnings, Warning{
			Line:    lineByField[profileWarning.Field],
			Field:   profileWarning.Field,
			Message: profileWarning.Message,
		})
	}
	return normalized, warnings, nil
}

func decodeProfileStringList(node *yaml.Node) ([]string, bool) {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil, false
	}
	result := make([]string, 0, len(node.Content))
	for _, child := range node.Content {
		if child.Kind != yaml.ScalarNode || child.Tag != "!!str" {
			return nil, false
		}
		result = append(result, child.Value)
	}
	return result, true
}
