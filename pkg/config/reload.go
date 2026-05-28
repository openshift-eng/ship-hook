package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
)

func StartReloadLoop(ctx context.Context, resolver *Resolver, configPath, supplementalDir string, pollInterval time.Duration, logger *logrus.Entry) {
	logger = logger.WithField("component", "config-reload")

	reload := func() {
		newCfg, err := Load(configPath, supplementalDir)
		if err != nil {
			logger.WithError(err).Error("Failed to reload config, keeping current config")
			return
		}
		if reflect.DeepEqual(resolver.Config(), newCfg) {
			return
		}
		resolver.UpdateConfig(newCfg)
		logger.Info("Config reloaded successfully")
	}

	watcher, watchDirs := setupWatcher(logger, configPath, supplementalDir)
	if watcher != nil {
		defer func() { _ = watcher.Close() }()
		logger.WithField("dirs", watchDirs).Info("Watching config directories for changes")
	} else {
		logger.Warn("File watching unavailable, using polling only")
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	defer debounce.Stop()

	var fsEvents <-chan fsnotify.Event
	var fsErrors <-chan error
	if watcher != nil {
		fsEvents = watcher.Events
		fsErrors = watcher.Errors
	}

	logger.WithField("poll_interval", pollInterval.String()).Info("Config reload loop started")

	for {
		select {
		case <-ctx.Done():
			logger.Info("Config reload loop stopped")
			return

		case event, ok := <-fsEvents:
			if !ok {
				fsEvents = nil
				continue
			}
			if isRelevantEvent(event, watchDirs) {
				debounce.Reset(time.Second)
			}

		case err, ok := <-fsErrors:
			if !ok {
				fsErrors = nil
				continue
			}
			logger.WithError(err).Error("File watcher error")

		case <-debounce.C:
			reload()

		case <-ticker.C:
			reload()
		}
	}
}

func setupWatcher(logger *logrus.Entry, configPath, supplementalDir string) (*fsnotify.Watcher, []watchedDir) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.WithError(err).Warn("Failed to create file watcher")
		return nil, nil
	}

	var dirs []watchedDir

	if configPath != "" {
		dir := filepath.Dir(configPath)
		if err := watcher.Add(dir); err != nil {
			logger.WithError(err).WithField("dir", dir).Warn("Failed to watch config directory")
		} else {
			dirs = append(dirs, watchedDir{path: dir, configMap: isConfigMapMount(dir)})
		}
	}

	if supplementalDir != "" && supplementalDir != filepath.Dir(configPath) {
		if err := watcher.Add(supplementalDir); err != nil {
			logger.WithError(err).WithField("dir", supplementalDir).Warn("Failed to watch supplemental directory")
		} else {
			dirs = append(dirs, watchedDir{path: supplementalDir, configMap: isConfigMapMount(supplementalDir)})
		}
	}

	if len(dirs) == 0 {
		_ = watcher.Close()
		return nil, nil
	}

	return watcher, dirs
}

type watchedDir struct {
	path      string
	configMap bool
}

func isConfigMapMount(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() == "..data" {
			return true
		}
	}
	return false
}

func isRelevantEvent(event fsnotify.Event, dirs []watchedDir) bool {
	for _, d := range dirs {
		if event.Name != d.path && !strings.HasPrefix(event.Name, d.path+string(os.PathSeparator)) {
			continue
		}
		if d.configMap {
			if event.Name == filepath.Join(d.path, "..data") && event.Has(fsnotify.Create) {
				return true
			}
			continue
		}
		ext := filepath.Ext(event.Name)
		if ext == ".yaml" || ext == ".yml" {
			return event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove)
		}
	}
	return false
}
