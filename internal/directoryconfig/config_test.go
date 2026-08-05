package directoryconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testOrigin = "https://directory.example"

func testConfigBody(t *testing.T) string {
	t.Helper()
	key, err := filepath.Abs(filepath.Join("..", "..", "misc", "test", "testKey.pem"))
	if err != nil {
		t.Fatal(err)
	}
	return "# leading comment\n" +
		"ACTOR_PEM: " + key + "\n" +
		"RELAY_DOMAIN: relay.example\n" +
		"UNRELATED:\n  nested: keep\n" +
		"DIRECTORIES:\n" +
		"  # directory comment\n" +
		"  - origin: https://directory.example\n" +
		"    enabled: true\n" +
		"  - origin: https://directory2.example\n" +
		"    enabled: false\n"
}

func writeTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(testConfigBody(t)), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFileUsesOnlyDirectoryIdentityConfiguration(t *testing.T) {
	path := writeTestConfig(t)
	config, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.Source != SourceFile || config.PublicBaseURL != "https://relay.example" ||
		config.RelayActor != "https://relay.example/actor" || config.KeyID != "https://relay.example/actor#main-key" ||
		len(config.Directories) != 2 || !config.Directories[0].Enabled || config.PrivateKey == nil {
		t.Fatalf("Load() = %#v", config)
	}
}

func TestDisablePreservesStructureCommentsModeOwnershipAndBackup(t *testing.T) {
	path := writeTestConfig(t)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := DisableFile(path, testOrigin)
	if err != nil {
		t.Fatalf("DisableFile() error = %v", err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatalf("Load() after disable = %v", err)
	}
	entry, err := config.Directory(testOrigin)
	if err != nil || entry.Enabled {
		t.Fatalf("disabled entry = (%#v, %v)", entry, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode() != after.Mode() || !sameOwnership(before, after) {
		t.Fatalf("metadata changed: before=%v after=%v", before.Mode(), after.Mode())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "# leading comment") || !strings.Contains(text, "# directory comment") ||
		!strings.Contains(text, "UNRELATED:") || !strings.Contains(text, "nested: keep") {
		t.Fatalf("unrelated structure or comments lost:\n%s", text)
	}
	backupBody, err := os.ReadFile(backup)
	if err != nil || string(backupBody) != testConfigBody(t) {
		t.Fatalf("backup = (%q, %v)", backupBody, err)
	}
}

func TestInjectedEditFailuresLeaveOriginalOrDisabled(t *testing.T) {
	for _, stage := range []writeStage{
		stagePrepared, stageBackupDurable, stageRenamed, stageDirectorySynced,
	} {
		t.Run(string(rune('0'+stage)), func(t *testing.T) {
			path := writeTestConfig(t)
			_, err := editFile(path, testOrigin, disableEntry, false, func(current writeStage) error {
				if current == stage {
					return errors.New("injected")
				}
				return nil
			})
			if err == nil {
				t.Fatal("injected failure was ignored")
			}
			config, loadErr := Load(path)
			if loadErr != nil {
				t.Fatalf("configuration became invalid: %v", loadErr)
			}
			entry, entryErr := config.Directory(testOrigin)
			if entryErr != nil {
				t.Fatalf("directory disappeared: %v", entryErr)
			}
			wantEnabled := stage == stagePrepared || stage == stageBackupDurable
			if entry.Enabled != wantEnabled {
				t.Fatalf("enabled = %t, want %t at stage %d", entry.Enabled, wantEnabled, stage)
			}
		})
	}
}

func TestRemovePreservesOriginalEnabledBackup(t *testing.T) {
	path := writeTestConfig(t)
	backup, err := DisableFile(path, testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	originalBackup, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveFile(path, testOrigin); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Directory(testOrigin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed Directory() error = %v", err)
	}
	retainedBackup, err := os.ReadFile(backup)
	if err != nil || string(retainedBackup) != string(originalBackup) {
		t.Fatal("remove replaced the recoverable pre-disable backup")
	}
}

func TestRepeatedDisablePreservesOriginalEnabledBackup(t *testing.T) {
	path := writeTestConfig(t)
	backup, err := DisableFile(path, testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DisableFile(path, testOrigin); err != nil {
		t.Fatal(err)
	}
	repeated, err := os.ReadFile(backup)
	if err != nil || string(repeated) != string(original) {
		t.Fatal("repeated disable replaced the pre-disable backup")
	}
}

func TestLoadAndEditRejectSymlinksAndDuplicateYAMLKeys(t *testing.T) {
	path := writeTestConfig(t)
	link := filepath.Join(t.TempDir(), "config.yml")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(link); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("Load(symlink) error = %v", err)
	}
	if _, err := DisableFile(link, testOrigin); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("DisableFile(symlink) error = %v", err)
	}
	duplicate := filepath.Join(t.TempDir(), "duplicate.yml")
	if err := os.WriteFile(duplicate, []byte(testConfigBody(t)+"RELAY_DOMAIN: other.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(duplicate); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("Load(duplicate) error = %v", err)
	}
}

func TestEnvironmentSourceIsExplicitAndNeverCreatesMissingConfig(t *testing.T) {
	keyPath, err := filepath.Abs(filepath.Join("..", "..", "misc", "test", "testKey.pem"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTOR_PEM", keyPath)
	t.Setenv("RELAY_DOMAIN", "relay.example")
	t.Setenv("DIRECTORIES", `[{origin: "https://directory.example", enabled: true}]`)
	path := filepath.Join(t.TempDir(), "missing.yml")
	config, err := Load(path)
	if err != nil || config.Source != SourceEnvironment || len(config.Directories) != 1 ||
		!config.Directories[0].Enabled {
		t.Fatalf("Load(environment) = (%#v, %v)", config, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing configuration was created: %v", err)
	}
}

func TestConcurrentDisablesDoNotLoseEitherDecision(t *testing.T) {
	path := writeTestConfig(t)
	origins := []string{testOrigin, "https://directory2.example"}
	var wait sync.WaitGroup
	errorsByIndex := make([]error, len(origins))
	for index, origin := range origins {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, errorsByIndex[index] = DisableFile(path, origin)
		}()
	}
	wait.Wait()
	for _, err := range errorsByIndex {
		if err != nil {
			t.Fatalf("DisableFile() error = %v", err)
		}
	}
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range origins {
		entry, err := config.Directory(origin)
		if err != nil || entry.Enabled {
			t.Fatalf("entry %s = (%#v, %v)", origin, entry, err)
		}
	}
}

func sameOwnership(left, right os.FileInfo) bool {
	leftUID, leftGID, leftErr := ownership(left)
	rightUID, rightGID, rightErr := ownership(right)
	return leftErr == nil && rightErr == nil && leftUID == rightUID && leftGID == rightGID
}
