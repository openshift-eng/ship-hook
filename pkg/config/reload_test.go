package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestStartReloadLoop(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(configPath, []byte("plugins:\n  - name: p\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewResolver(cfg)

	if !resolver.IsEnabled("p", "org", "repo") {
		t.Fatal("expected p enabled initially")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := logrus.NewEntry(logrus.New())
	go StartReloadLoop(ctx, resolver, configPath, "", 50*time.Millisecond, logger)

	if err := os.WriteFile(configPath, []byte("plugins:\n  - name: p\n    disabled: true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(5 * time.Second)
	for {
		if !resolver.IsEnabled("p", "org", "repo") {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for config reload")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestStartReloadLoop_ErrorKeepsOldConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(configPath, []byte("plugins:\n  - name: p\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewResolver(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := logrus.NewEntry(logrus.New())
	go StartReloadLoop(ctx, resolver, configPath, "", 50*time.Millisecond, logger)

	if err := os.WriteFile(configPath, []byte("{{{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(300 * time.Millisecond)

	if !resolver.IsEnabled("p", "org", "repo") {
		t.Fatal("expected p still enabled after reload error")
	}
}

func TestStartReloadLoop_StopsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("plugins: []\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewResolver(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	logger := logrus.NewEntry(logrus.New())

	done := make(chan struct{})
	go func() {
		StartReloadLoop(ctx, resolver, configPath, "", 50*time.Millisecond, logger)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reload loop did not stop after context cancellation")
	}
}

func TestIsConfigMapMount(t *testing.T) {
	t.Run("regular directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("plugins: []\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if isConfigMapMount(dir) {
			t.Error("expected false for regular directory")
		}
	})

	t.Run("ConfigMap mount", func(t *testing.T) {
		dir := t.TempDir()
		dataDir := filepath.Join(dir, "..2024_01_01")
		if err := os.Mkdir(dataDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dataDir, filepath.Join(dir, "..data")); err != nil {
			t.Fatal(err)
		}
		if !isConfigMapMount(dir) {
			t.Error("expected true for ConfigMap mount directory")
		}
	})
}
