package directorycommand

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"github.com/thystra/Activity-Relay/internal/directoryconfig"
)

const environmentAcknowledgementFlag = "acknowledge-external-disable"

type dependencies struct {
	load    func(string) (directoryconfig.Config, error)
	disable func(string, string) (string, error)
	remove  func(string, string) (string, error)
	client  func(directoryconfig.Config, string) (*directoryclient.Client, error)
	sleep   func(context.Context, time.Duration) error
}

func productionDependencies() dependencies {
	return dependencies{
		load:    directoryconfig.Load,
		disable: directoryconfig.DisableFile,
		remove:  directoryconfig.RemoveFile,
		client: func(config directoryconfig.Config, origin string) (*directoryclient.Client, error) {
			return directoryclient.New(directoryclient.Options{
				Origin:        origin,
				RelayActor:    config.RelayActor,
				PublicBaseURL: config.PublicBaseURL,
				KeyID:         config.KeyID,
				PrivateKey:    config.PrivateKey,
			})
		},
		sleep: sleepContext,
	}
}

// BuildCommand returns the explicit, operator-invoked directory command tree.
// It has no startup hook or scheduling side effect.
func BuildCommand() *cobra.Command {
	return buildCommand(productionDependencies())
}

func buildCommand(deps dependencies) *cobra.Command {
	directory := &cobra.Command{
		Use:   "directory",
		Short: "Manage explicit Activity-Relay Directory lifecycle operations",
	}

	status := &cobra.Command{
		Use:               "status [origin]",
		Short:             "List local entries or query one directory status",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeOrigins(deps),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := loadCommandConfig(cmd, deps)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				entries := append([]directoryclient.Directory(nil), config.Directories...)
				sort.Slice(entries, func(left, right int) bool {
					return entries[left].Origin < entries[right].Origin
				})
				for _, entry := range entries {
					state := "disabled"
					if entry.Enabled {
						state = "enabled"
					}
					cmd.Printf("%s %s\n", entry.Origin, state)
				}
				return nil
			}
			entry, client, err := selectedClient(config, args[0], deps, false)
			if err != nil {
				return err
			}
			remote, err := retryStatus(cmd.Context(), deps, client.Status)
			if err != nil {
				return commandError("status", err)
			}
			cmd.Printf(
				"%s enabled=%t lifecycle_enabled=%t lifecycle_available=%t enrollment_open=%t version=%s\n",
				entry.Origin,
				entry.Enabled,
				remote.LifecycleEnabled,
				remote.LifecycleAvailable,
				remote.EnrollmentOpen,
				remote.Version,
			)
			return nil
		},
	}
	directory.AddCommand(status)

	directory.AddCommand(lifecycleCommand(
		"register [origin]",
		"Register an explicitly enabled relay",
		deps,
		func(ctx context.Context, client *directoryclient.Client) (directoryclient.Response, error) {
			return client.Register(ctx)
		},
	))
	directory.AddCommand(lifecycleCommand(
		"heartbeat [origin]",
		"Record a heartbeat for an explicitly enabled relay",
		deps,
		func(ctx context.Context, client *directoryclient.Client) (directoryclient.Response, error) {
			return client.Heartbeat(ctx)
		},
	))
	directory.AddCommand(lifecycleCommand(
		"sync [origin]",
		"Heartbeat and reconcile one explicit missing registration",
		deps,
		func(ctx context.Context, client *directoryclient.Client) (directoryclient.Response, error) {
			return client.HeartbeatWithRegisterReconciliation(ctx)
		},
	))
	directory.AddCommand(unregisterCommand(deps))
	return directory
}

type lifecycleOperation func(context.Context, *directoryclient.Client) (directoryclient.Response, error)

func lifecycleCommand(
	use, short string,
	deps dependencies,
	operation lifecycleOperation,
) *cobra.Command {
	command := &cobra.Command{
		Use:               use,
		Short:             short,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeOrigins(deps),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := loadCommandConfig(cmd, deps)
			if err != nil {
				return err
			}
			entry, client, err := selectedClient(config, args[0], deps, true)
			if err != nil {
				return err
			}
			response, err := retryLifecycle(cmd.Context(), deps, func(ctx context.Context) (directoryclient.Response, error) {
				return operation(ctx, client)
			})
			if err != nil {
				return commandError(cmd.Name(), err)
			}
			cmd.Printf("%s %s: %s\n", cmd.Name(), entry.Origin, response.Outcome)
			return nil
		},
	}
	return command
}

func unregisterCommand(deps dependencies) *cobra.Command {
	var remove, acknowledge bool
	command := &cobra.Command{
		Use:               "unregister [origin]",
		Short:             "Durably disable an entry before remote unregister",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeOrigins(deps),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := loadCommandConfig(cmd, deps)
			if err != nil {
				return err
			}
			entry, client, err := selectedClient(config, args[0], deps, false)
			if err != nil {
				return err
			}
			if config.Source == directoryconfig.SourceFile {
				backup, err := deps.disable(config.Path, entry.Origin)
				if err != nil {
					return errors.New("directory entry could not be durably disabled; no remote request was sent")
				}
				cmd.Printf("disabled %s; backup=%s\n", entry.Origin, backup)
			} else {
				if remove {
					return errors.New("--remove requires a regular configuration file")
				}
				if !acknowledge {
					return fmt.Errorf(
						"environment-only configuration cannot be mutated; disable the external source and rerun with --%s",
						environmentAcknowledgementFlag,
					)
				}
				cmd.PrintErrln("warning: disable this directory in the external configuration source before restart")
			}
			response, err := retryLifecycle(cmd.Context(), deps, client.Unregister)
			if err != nil {
				if config.Source == directoryconfig.SourceFile {
					cmd.PrintErrln("remote unregister failed; the file-backed entry remains disabled; rerun unregister to retry")
				} else {
					cmd.PrintErrln("remote unregister failed; keep the external entry disabled and rerun unregister to retry")
				}
				return commandError("unregister", err)
			}
			cmd.Printf("unregister %s: %s\n", entry.Origin, response.Outcome)
			if remove {
				if _, err := deps.remove(config.Path, entry.Origin); err != nil {
					return errors.New("remote unregister succeeded but the disabled entry could not be removed")
				}
				cmd.Printf("removed %s from configuration\n", entry.Origin)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&remove, "remove", false, "remove the disabled entry after remote success")
	command.Flags().BoolVar(
		&acknowledge,
		environmentAcknowledgementFlag,
		false,
		"acknowledge that an external configuration source must be disabled separately",
	)
	return command
}

func selectedClient(
	config directoryconfig.Config,
	origin string,
	deps dependencies,
	requireEnabled bool,
) (directoryclient.Directory, *directoryclient.Client, error) {
	entry, err := config.Directory(origin)
	if err != nil {
		return directoryclient.Directory{}, nil, errors.New("directory entry is not configured")
	}
	if requireEnabled && !entry.Enabled {
		return directoryclient.Directory{}, nil, errors.New("directory entry is disabled")
	}
	client, err := deps.client(config, entry.Origin)
	if err != nil {
		return directoryclient.Directory{}, nil, errors.New("directory client could not be initialized")
	}
	return entry, client, nil
}

func loadCommandConfig(cmd *cobra.Command, deps dependencies) (directoryconfig.Config, error) {
	path, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		path, err = cmd.Flags().GetString("config")
	}
	if err != nil {
		return directoryconfig.Config{}, errors.New("configuration path is unavailable")
	}
	config, err := deps.load(path)
	if err != nil {
		return directoryconfig.Config{}, errors.New("directory command configuration is invalid")
	}
	return config, nil
}

func completeOrigins(deps dependencies) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		config, err := loadCommandConfig(cmd, deps)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		values := make([]string, 0, len(config.Directories))
		for _, entry := range config.Directories {
			if len(toComplete) <= len(entry.Origin) && entry.Origin[:len(toComplete)] == toComplete {
				values = append(values, entry.Origin)
			}
		}
		sort.Strings(values)
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}

func commandError(operation string, err error) error {
	var protocolError *directoryclient.ProtocolError
	if errors.As(err, &protocolError) {
		return fmt.Errorf("directory %s failed: %s", operation, protocolError.Code)
	}
	switch {
	case errors.Is(err, directoryclient.ErrDirectoryTransport):
		return fmt.Errorf("directory %s failed: transport unavailable", operation)
	case errors.Is(err, directoryclient.ErrResponseTooLarge):
		return fmt.Errorf("directory %s failed: response too large", operation)
	default:
		return fmt.Errorf("directory %s failed: invalid response", operation)
	}
}
