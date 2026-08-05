package api

import (
	"context"
	"errors"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"github.com/thystra/Activity-Relay/internal/directoryconfig"
	"github.com/thystra/Activity-Relay/internal/directoryscheduler"
	"github.com/thystra/Activity-Relay/models"
)

func newDirectoryScheduler(config *models.RelayConfig) (*directoryscheduler.Scheduler, error) {
	if config == nil || !config.DirectorySchedulerEnabled() || config.ConfigurationPath() == "" {
		return nil, errors.New("directory scheduler is not configured")
	}
	info, err := os.Lstat(config.ConfigurationPath())
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("directory scheduler requires a regular configuration file")
	}
	directories := config.Directories()
	entries := make([]directoryclient.Directory, len(directories))
	for index, entry := range directories {
		entries[index] = directoryclient.Directory{Origin: entry.Origin, Enabled: entry.Enabled}
	}
	if len(entries) == 0 {
		return nil, errors.New("directory scheduler has no entries")
	}
	store, err := directoryscheduler.NewRedisStore(config.RedisClient())
	if err != nil {
		return nil, err
	}
	actor := models.NewActivityPubActorFromRelayConfig(config)
	path := config.ConfigurationPath()
	return directoryscheduler.New(directoryscheduler.Config{
		RelayActor:  actor.ID,
		Directories: entries,
		Store:       store,
		Enabled: func(origin string) (bool, error) {
			current, err := directoryconfig.Load(path)
			if err != nil || current.Source != directoryconfig.SourceFile {
				return false, errors.New("durable directory configuration is unavailable")
			}
			return durableDirectoryEnabled(current, origin)
		},
		Clients: func(entry directoryclient.Directory) (directoryscheduler.Client, error) {
			return directoryclient.New(directoryclient.Options{
				Origin:        entry.Origin,
				RelayActor:    actor.ID,
				PublicBaseURL: config.ServerHostname().String(),
				KeyID:         actor.PublicKey.ID,
				PrivateKey:    config.ActorKey(),
			})
		},
		Metrics: OperationalMetrics,
	})
}

func durableDirectoryEnabled(config directoryconfig.Config, origin string) (bool, error) {
	if !config.SchedulerEnabled {
		return false, nil
	}
	entry, err := config.Directory(origin)
	if errors.Is(err, directoryconfig.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return entry.Enabled, nil
}

func startDirectoryScheduler(ctx context.Context, config *models.RelayConfig) <-chan struct{} {
	done := make(chan struct{})
	if config == nil || !config.DirectorySchedulerEnabled() {
		close(done)
		return done
	}
	scheduler, err := newDirectoryScheduler(config)
	if err != nil {
		// Scheduling is optional and must never block ActivityPub startup.
		logrus.Warn("Directory scheduler is unavailable; relay service will continue")
		close(done)
		return done
	}
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()
	return done
}
