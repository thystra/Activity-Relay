package directoryconfig

import (
	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"gopkg.in/yaml.v3"
)

var profileKeys = map[string]struct{}{
	"participation_mode": {}, "availability": {}, "relay_type": {},
	"languages": {}, "countries": {}, "regions": {}, "topics": {},
	"contact_fediverse": {}, "contact_email": {}, "contact_url": {},
	"participation_url": {}, "notes": {},
}

func profileSetting(root *yaml.Node) (directoryclient.RelayProfile, error) {
	node := mappingValue(root, "DIRECTORY_PROFILE")
	if node == nil {
		return directoryclient.RelayProfile{}, nil
	}
	return decodeProfileNode(node)
}

func parseEnvironmentProfile(value string) (directoryclient.RelayProfile, error) {
	if value == "" {
		return directoryclient.RelayProfile{}, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(value), &document); err != nil || len(document.Content) != 1 {
		return directoryclient.RelayProfile{}, ErrConfiguration
	}
	if err := validateNode(document.Content[0]); err != nil {
		return directoryclient.RelayProfile{}, err
	}
	return decodeProfileNode(document.Content[0])
}

func decodeProfileNode(node *yaml.Node) (directoryclient.RelayProfile, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return directoryclient.RelayProfile{}, ErrConfiguration
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if _, ok := profileKeys[node.Content[index].Value]; !ok {
			return directoryclient.RelayProfile{}, ErrConfiguration
		}
	}
	var profile directoryclient.RelayProfile
	if err := node.Decode(&profile); err != nil {
		return directoryclient.RelayProfile{}, ErrConfiguration
	}
	normalized, err := directoryclient.NormalizeRelayProfile(profile)
	if err != nil {
		return directoryclient.RelayProfile{}, ErrConfiguration
	}
	return normalized, nil
}
