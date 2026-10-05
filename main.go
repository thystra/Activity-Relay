/*
Yet another powerful customizable ActivityPub relay server written in Go.

# Run Activity-Relay

API Server

	./Activity-Relay --config /path/to/config.yml server

Job Worker

	./Activity-Relay --config /path/to/config.yml worker

CLI Management Utility

	./Activity-Relay --config /path/to/config.yml control

Manual Directory Lifecycle Utility

	./Activity-Relay --config /path/to/config.yml directory status

# Config

YAML Format

	ACTOR_PEM: /var/lib/relay/actor.pem
	REDIS_URL: redis://localhost:6379
	RELAY_BIND: 0.0.0.0:8080
	# OBSERVABILITY_BIND: 127.0.0.1:9090
	RELAY_DOMAIN: relay.example.org
	RELAY_SERVICENAME: Example ActivityPub Relay
	JOB_CONCURRENCY: 50
	OUTBOUND_SIGNATURE_PROFILE: dual
	PUBLIC_ADDRESS_DISTRIBUTION_POLICY: explicit_public_only
	DIRECTORY_SCHEDULER_ENABLED: false
	# DIRECTORIES:
	#   - origin: https://directory.example.org
	#     enabled: false
	RELAY_SUMMARY: |
		Example ActivityPub Relay is powered by Activity-Relay
	RELAY_ICON: https://example.com/example_icon.png
	RELAY_IMAGE: https://example.com/example_image.png

# Environment Variable

This is Optional : When config file not exist, use environment variables.
  - ACTOR_PEM
  - REDIS_URL
  - RELAY_BIND
  - OBSERVABILITY_BIND
  - RELAY_DOMAIN
  - RELAY_SERVICENAME
  - JOB_CONCURRENCY
  - OUTBOUND_SIGNATURE_PROFILE
  - PUBLIC_ADDRESS_DISTRIBUTION_POLICY
  - RELAY_SUMMARY
  - RELAY_ICON
  - RELAY_IMAGE
*/
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/thystra/Activity-Relay/api"
	"github.com/thystra/Activity-Relay/control"
	"github.com/thystra/Activity-Relay/deliver"
	"github.com/thystra/Activity-Relay/internal/directorycommand"
	"github.com/thystra/Activity-Relay/internal/directoryconfig"
	"github.com/thystra/Activity-Relay/models"
)

var (
	version          = "devel"
	verbose          bool
	testConfig       bool
	strictConfigTest bool

	GlobalConfig *models.RelayConfig
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{
		ForceColors: true,
	})

	var app = buildCommand()
	app.PersistentFlags().StringP("config", "c", "config.yml", "Path of config")
	app.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show debug log")
	app.PersistentFlags().BoolVarP(&testConfig, "test-config", "t", false, "Test configuration and exit")
	app.PersistentFlags().BoolVar(&strictConfigTest, "strict", false, "With --test-config, treat optional metadata warnings as errors")

	if err := app.Execute(); err != nil {
		os.Exit(1)
	}
}

func serverLifecycleContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

func buildCommand() *cobra.Command {
	var keyOutput string
	var keyBits int
	var generateKey = &cobra.Command{
		Use:   "generate-key",
		Short: "Generate the relay actor identity key",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := models.GenerateActorKey(keyOutput, keyBits); err != nil {
				return err
			}
			fmt.Printf("Generated relay actor key at %s\n", keyOutput)
			return nil
		},
	}
	generateKey.Flags().StringVarP(&keyOutput, "output", "o", "actor.pem", "Output path; must not already exist")
	generateKey.Flags().IntVar(&keyBits, "bits", 3072, "RSA key size (minimum 2048)")

	var server = &cobra.Command{
		Use:   "server",
		Short: "Activity-Relay API Server",
		Long:  "Activity-Relay API Server is providing WebFinger API, ActivityPub inbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			initConfig(cmd)
			fmt.Println(GlobalConfig.DumpWelcomeMessage("API Server", version))
			ctx, stop := serverLifecycleContext(cmd.Context())
			defer stop()
			err := api.EntrypointContext(ctx, GlobalConfig, version)
			if err != nil {
				logrus.Fatal(err.Error())
			}
			return nil
		},
	}

	var worker = &cobra.Command{
		Use:   "worker",
		Short: "Activity-Relay Job Worker",
		Long:  "Activity-Relay Job Worker is providing ActivityPub Activity deliverer",
		RunE: func(cmd *cobra.Command, args []string) error {
			initConfig(cmd)
			fmt.Println(GlobalConfig.DumpWelcomeMessage("Job Worker", version))
			err := deliver.Entrypoint(GlobalConfig, version)
			if err != nil {
				logrus.Fatal(err.Error())
			}
			return nil
		},
	}

	var command = &cobra.Command{
		Use:   "control",
		Short: "Activity-Relay CLI",
		Long:  "Activity-Relay CLI Management Utility",
	}
	control.BuildCommand(command)

	var app = &cobra.Command{
		Use:     "relay",
		Short:   "Activity-Relay",
		Long:    "Activity-Relay - ActivityPub Relay Server",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !testConfig {
				return cmd.Help()
			}
			return runConfigTest(cmd)
		},
	}
	app.AddCommand(server)
	app.AddCommand(worker)
	app.AddCommand(command)
	app.AddCommand(generateKey)
	app.AddCommand(directorycommand.BuildCommand())

	return app
}

func runConfigTest(cmd *cobra.Command) error {
	if cmd == nil {
		return errors.New("configuration test command is unavailable")
	}
	configPath := cmd.Flag("config").Value.String()
	viper.Reset()
	file, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("activity-relay: configuration file %s cannot be opened: %w", configPath, err)
	}
	defer file.Close()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(file); err != nil {
		return fmt.Errorf("activity-relay: configuration file %s syntax is invalid: %w", configPath, err)
	}
	if err := models.ValidateRelayConfig(); err != nil {
		return fmt.Errorf("activity-relay: configuration file %s is invalid: %w", configPath, err)
	}
	directoryConfig, err := directoryconfig.Load(configPath)
	if err != nil {
		return fmt.Errorf("activity-relay: Directory configuration in %s is invalid: %w", configPath, err)
	}
	fmt.Printf("activity-relay: configuration file %s syntax is ok\n", configPath)
	for _, warning := range directoryConfig.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "activity-relay: warning: %s: %s\n", configPath, warning.String())
	}
	if strictConfigTest && len(directoryConfig.Warnings) != 0 {
		return errors.New("activity-relay: configuration test completed with warnings (--strict)")
	}
	if len(directoryConfig.Warnings) != 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "activity-relay: configuration test completed with warnings")
	} else {
		fmt.Println("activity-relay: configuration test is successful")
	}
	return nil
}

func initConfig(cmd *cobra.Command) {
	if verbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	configPath := cmd.Flag("config").Value.String()
	file, err := os.Open(configPath)

	if err == nil {
		defer file.Close()
		viper.SetConfigType("yaml")
		if err := viper.ReadConfig(file); err != nil {
			logrus.Fatal("Configuration file is invalid: ", err.Error())
		}
	} else {
		logrus.Warn("Config file not exist. Use environment variables.")

		viper.BindEnv("ACTOR_PEM")
		viper.BindEnv("REDIS_URL")
		viper.BindEnv("RELAY_BIND")
		viper.BindEnv("OBSERVABILITY_BIND")
		viper.BindEnv("RELAY_DOMAIN")
		viper.BindEnv("RELAY_SERVICENAME")
		viper.BindEnv("JOB_CONCURRENCY")
		viper.BindEnv("RELAY_SUMMARY")
		viper.BindEnv("RELAY_ICON")
		viper.BindEnv("RELAY_IMAGE")
		viper.BindEnv("MAX_ACTIVITY_BYTES")
		viper.BindEnv("MAX_FANOUT_TARGETS")
		viper.BindEnv("MAX_QUEUE_JOBS")
		viper.BindEnv("OUTBOUND_SIGNATURE_PROFILE")
		viper.BindEnv("PUBLIC_ADDRESS_DISTRIBUTION_POLICY")
	}

	GlobalConfig, err = models.NewRelayConfig()
	if err != nil {
		logrus.Fatal(err.Error())
	}
	GlobalConfig.SetConfigurationPath(configPath)
	if directoryConfig, directoryErr := directoryconfig.Load(configPath); directoryErr == nil {
		for _, warning := range directoryConfig.Warnings {
			logrus.WithField("configuration", configPath).Warn(warning.String())
		}
	} else {
		logrus.Warn("Directory-specific configuration validation failed; optional Directory metadata will be ignored and the scheduler will remain fail-closed")
	}
}
