package directoryconfig

import (
	"bytes"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"github.com/thystra/Activity-Relay/models"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const maximumConfigBytes = int64(1024 * 1024)

var (
	ErrConfiguration = errors.New("directory command configuration is invalid")
	ErrNotFound      = errors.New("directory entry was not found")
)

type Source uint8

const (
	SourceFile Source = iota + 1
	SourceEnvironment
)

// Config contains only the identity and directory fields needed by manual
// directory commands. It deliberately does not initialize Redis or workers.
type Config struct {
	Source           Source
	Path             string
	Directories      []directoryclient.Directory
	PublicBaseURL    string
	RelayActor       string
	KeyID            string
	PrivateKey       *rsa.PrivateKey
	SchedulerEnabled bool
	RedisURL         string
}

func Load(path string) (Config, error) {
	if path == "" || strings.TrimSpace(path) != path {
		return Config{}, ErrConfiguration
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return Config{}, ErrConfiguration
		}
		body, err := readRegularNoFollow(path, info)
		if err != nil {
			return Config{}, ErrConfiguration
		}
		root, err := decodeYAML(body)
		if err != nil {
			return Config{}, ErrConfiguration
		}
		actorPEM, ok := scalarSetting(root, "ACTOR_PEM")
		if !ok {
			return Config{}, ErrConfiguration
		}
		relayDomain, ok := scalarSetting(root, "RELAY_DOMAIN")
		if !ok {
			return Config{}, ErrConfiguration
		}
		directories, err := directoriesSetting(root)
		if err != nil {
			return Config{}, err
		}
		schedulerEnabled, err := boolSetting(root, "DIRECTORY_SCHEDULER_ENABLED")
		if err != nil {
			return Config{}, err
		}
		redisURL := ""
		redisNode := mappingValue(root, "REDIS_URL")
		if redisNode != nil {
			if redisNode.Kind != yaml.ScalarNode || redisNode.Tag != "!!str" ||
				redisNode.Value == "" || strings.TrimSpace(redisNode.Value) != redisNode.Value {
				return Config{}, ErrConfiguration
			}
			redisURL = redisNode.Value
		}
		if schedulerEnabled && redisURL == "" {
			return Config{}, ErrConfiguration
		}
		return build(SourceFile, path, actorPEM, relayDomain, directories, schedulerEnabled, redisURL)
	case errors.Is(err, os.ErrNotExist):
		directories, err := parseEnvironmentDirectories(os.Getenv("DIRECTORIES"))
		if err != nil {
			return Config{}, err
		}
		return build(
			SourceEnvironment,
			path,
			os.Getenv("ACTOR_PEM"),
			os.Getenv("RELAY_DOMAIN"),
			directories,
			false,
			"",
		)
	default:
		return Config{}, ErrConfiguration
	}
}

func build(
	source Source,
	path, actorPEM, relayDomain string,
	directories []directoryclient.Directory,
	schedulerEnabled bool,
	redisURL string,
) (Config, error) {
	if actorPEM == "" || strings.TrimSpace(actorPEM) != actorPEM ||
		relayDomain == "" || strings.TrimSpace(relayDomain) != relayDomain {
		return Config{}, ErrConfiguration
	}
	base, err := directoryclient.ParseOrigin("https://" + relayDomain)
	if err != nil {
		return Config{}, ErrConfiguration
	}
	key, err := models.LoadActorPrivateKey(actorPEM)
	if err != nil {
		return Config{}, errors.New("relay actor identity could not be loaded")
	}
	actor := base.String() + "/actor"
	return Config{
		Source:           source,
		Path:             path,
		Directories:      directories,
		PublicBaseURL:    base.String(),
		RelayActor:       actor,
		KeyID:            actor + "#main-key",
		PrivateKey:       key,
		SchedulerEnabled: schedulerEnabled,
		RedisURL:         redisURL,
	}, nil
}

func boolSetting(root *yaml.Node, name string) (bool, error) {
	node := mappingValue(root, name)
	if node == nil {
		return false, nil
	}
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
		return false, ErrConfiguration
	}
	var value bool
	if err := node.Decode(&value); err != nil {
		return false, ErrConfiguration
	}
	return value, nil
}

func (config Config) Directory(origin string) (directoryclient.Directory, error) {
	parsed, err := directoryclient.ParseOrigin(origin)
	if err != nil {
		return directoryclient.Directory{}, ErrConfiguration
	}
	for _, entry := range config.Directories {
		if entry.Origin == parsed.String() {
			return entry, nil
		}
	}
	return directoryclient.Directory{}, ErrNotFound
}

func readRegularNoFollow(path string, expected os.FileInfo) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "configuration")
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrConfiguration
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(expected, actual) ||
		actual.Size() > maximumConfigBytes {
		return nil, ErrConfiguration
	}
	body, err := io.ReadAll(io.LimitReader(file, maximumConfigBytes+1))
	if err != nil || int64(len(body)) > maximumConfigBytes {
		return nil, ErrConfiguration
	}
	return body, nil
}

func decodeYAML(body []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil || len(document.Content) != 1 ||
		document.Content[0].Kind != yaml.MappingNode {
		return nil, ErrConfiguration
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrConfiguration
	}
	if err := validateNode(document.Content[0]); err != nil {
		return nil, err
	}
	return document.Content[0], nil
}

func validateNode(node *yaml.Node) error {
	if node == nil || node.Kind == yaml.AliasNode || node.Anchor != "" {
		return ErrConfiguration
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]struct{}, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			if index+1 >= len(node.Content) || node.Content[index].Kind != yaml.ScalarNode {
				return ErrConfiguration
			}
			name := node.Content[index].Value
			if _, exists := seen[name]; exists {
				return ErrConfiguration
			}
			seen[name] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := validateNode(child); err != nil {
			return err
		}
	}
	return nil
}

func scalarSetting(root *yaml.Node, name string) (string, bool) {
	node := mappingValue(root, name)
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!str" ||
		node.Value == "" || strings.TrimSpace(node.Value) != node.Value {
		return "", false
	}
	return node.Value, true
}

func directoriesSetting(root *yaml.Node) ([]directoryclient.Directory, error) {
	node := mappingValue(root, "DIRECTORIES")
	if node == nil {
		return nil, nil
	}
	return parseDirectorySequence(node)
}

func parseEnvironmentDirectories(value string) ([]directoryclient.Directory, error) {
	if value == "" {
		return nil, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(value), &document); err != nil || len(document.Content) != 1 {
		return nil, ErrConfiguration
	}
	if err := validateNode(document.Content[0]); err != nil {
		return nil, err
	}
	return parseDirectorySequence(document.Content[0])
}

func parseDirectorySequence(node *yaml.Node) ([]directoryclient.Directory, error) {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil, ErrConfiguration
	}
	entries := make([]directoryclient.Directory, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			return nil, ErrConfiguration
		}
		origin := mappingValue(item, "origin")
		if origin == nil || origin.Kind != yaml.ScalarNode || origin.Tag != "!!str" {
			return nil, ErrConfiguration
		}
		enabled := false
		if enabledNode := mappingValue(item, "enabled"); enabledNode != nil {
			if enabledNode.Kind != yaml.ScalarNode || enabledNode.Tag != "!!bool" {
				return nil, ErrConfiguration
			}
			if err := enabledNode.Decode(&enabled); err != nil {
				return nil, ErrConfiguration
			}
		}
		entries = append(entries, directoryclient.Directory{
			Origin: origin.Value, Enabled: enabled,
		})
	}
	parsed, err := directoryclient.ParseDirectories(entries)
	if err != nil {
		return nil, fmt.Errorf("%w: DIRECTORIES", ErrConfiguration)
	}
	return parsed, nil
}

func mappingValue(mapping *yaml.Node, name string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == name {
			return mapping.Content[index+1]
		}
	}
	return nil
}
