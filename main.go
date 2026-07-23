/*
Yet another powerful customizable ActivityPub relay server written in Go.

# Run Activity-Relay

API Server

	./Activity-Relay --config /path/to/config.yml server

Job Worker

	./Activity-Relay --config /path/to/config.yml worker

CLI Management Utility

	./Activity-Relay --config /path/to/config.yml control

# Config

YAML Format

	ACTOR_PEM: /var/lib/relay/actor.pem
	REDIS_URL: redis://localhost:6379
	RELAY_BIND: 0.0.0.0:8080
	RELAY_DOMAIN: relay.toot.yukimochi.jp
	RELAY_SERVICENAME: YUKIMOCHI Toot Relay Service
	JOB_CONCURRENCY: 50
	RELAY_SUMMARY: |
		YUKIMOCHI Toot Relay Service is Running by Activity-Relay
	RELAY_ICON: https://example.com/example_icon.png
	RELAY_IMAGE: https://example.com/example_image.png

# Environment Variable

This is Optional : When config file not exist, use environment variables.
  - ACTOR_PEM
  - REDIS_URL
  - RELAY_BIND
  - RELAY_DOMAIN
  - RELAY_SERVICENAME
  - JOB_CONCURRENCY
  - RELAY_SUMMARY
  - RELAY_ICON
  - RELAY_IMAGE
*/
package main

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/yukimochi/Activity-Relay/api"
	"github.com/yukimochi/Activity-Relay/control"
	"github.com/yukimochi/Activity-Relay/deliver"
	"github.com/yukimochi/Activity-Relay/models"
)

var (
	version = "devel"
	verbose bool

	GlobalConfig *models.RelayConfig
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{
		ForceColors: true,
	})

	var app = buildCommand()
	app.PersistentFlags().StringP("config", "c", "config.yml", "Path of config")
	app.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show debug log")

	if err := app.Execute(); err != nil {
		os.Exit(1)
	}
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
			err := api.Entrypoint(GlobalConfig, version)
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
		Short: "YUKIMOCHI Activity-Relay",
		Long:  "YUKIMOCHI Activity-Relay - ActivityPub Relay Server",
	}
	app.AddCommand(server)
	app.AddCommand(worker)
	app.AddCommand(command)
	app.AddCommand(generateKey)

	return app
}

func initConfig(cmd *cobra.Command) {
	if verbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	configPath := cmd.Flag("config").Value.String()
	file, err := os.Open(configPath)
	defer file.Close()

	if err == nil {
		viper.SetConfigType("yaml")
		viper.ReadConfig(file)
	} else {
		logrus.Warn("Config file not exist. Use environment variables.")

		viper.BindEnv("ACTOR_PEM")
		viper.BindEnv("REDIS_URL")
		viper.BindEnv("RELAY_BIND")
		viper.BindEnv("RELAY_DOMAIN")
		viper.BindEnv("RELAY_SERVICENAME")
		viper.BindEnv("JOB_CONCURRENCY")
		viper.BindEnv("RELAY_SUMMARY")
		viper.BindEnv("RELAY_ICON")
		viper.BindEnv("RELAY_IMAGE")
		viper.BindEnv("MAX_ACTIVITY_BYTES")
		viper.BindEnv("MAX_FANOUT_TARGETS")
		viper.BindEnv("MAX_QUEUE_JOBS")
	}

	GlobalConfig, err = models.NewRelayConfig()
	if err != nil {
		logrus.Fatal(err.Error())
	}
}
