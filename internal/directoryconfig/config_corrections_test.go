package directoryconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFileConfigRetainsRedisURLWhenSchedulerGateIsFalse(t *testing.T) {
	keyPath, err := filepath.Abs(filepath.Join("..", "..", "misc", "test", "testKey.pem"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yml")
	body := fmt.Sprintf(
		"ACTOR_PEM: %s\nRELAY_DOMAIN: relay.example\nREDIS_URL: redis://127.0.0.1:6379/0\nDIRECTORY_SCHEDULER_ENABLED: false\nDIRECTORIES:\n  - origin: https://directory.example\n    enabled: true\n",
		keyPath,
	)
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.SchedulerEnabled || config.RedisURL != "redis://127.0.0.1:6379/0" {
		t.Fatalf("Load() = %#v", config)
	}
}

func TestFileConfigRejectsMalformedRedisURLWhenGateIsFalse(t *testing.T) {
	keyPath, err := filepath.Abs(filepath.Join("..", "..", "misc", "test", "testKey.pem"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yml")
	body := fmt.Sprintf(
		"ACTOR_PEM: %s\nRELAY_DOMAIN: relay.example\nREDIS_URL: true\nDIRECTORY_SCHEDULER_ENABLED: false\n",
		keyPath,
	)
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("Load() error = %v", err)
	}
}
