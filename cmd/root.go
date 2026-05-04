// Package cmd contains the cobra command definitions for the scoutbook-xls
// CLI. root.go wires flags, environment variables, and an optional YAML
// config file into a Config struct that is handed to a RunnerFunc so the
// plumbing can be tested independently of the report orchestration.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Config holds the resolved configuration values assembled from flags,
// environment variables, and/or a YAML config file.
//
// BaseURL is the Scouting API base URL. It is not exposed via a flag because
// production callers always want "https://api.scouting.org"; it exists so
// integration tests can point Run at an httptest.Server. An empty value is
// treated as the default production URL.
type Config struct {
	Token     string
	OrgGuid   string
	DenType   string
	DenNumber string
	Output    string
	BaseURL   string
}

// RunnerFunc is the function signature the root command invokes once
// configuration has been resolved. Extracting this indirection lets tests
// assert on the Config without needing the real report runner.
type RunnerFunc func(ctx context.Context, cfg Config) error

// NewRootCmd builds the root cobra command. A fresh viper instance is
// created per invocation so that repeated test runs don't leak state
// through viper's package-level globals.
func NewRootCmd(runner RunnerFunc) *cobra.Command {
	v := viper.New()
	v.SetEnvPrefix("SCOUTBOOK")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	var configPath string

	cmd := &cobra.Command{
		Use:   "scoutbook-xls",
		Short: "Generate a den progress XLSX from Scoutbook data",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath != "" {
				v.SetConfigFile(configPath)
			} else {
				v.SetConfigName("scoutbook-xls")
				v.SetConfigType("yaml")
				v.AddConfigPath(".")
				if home, err := os.UserHomeDir(); err == nil && home != "" {
					v.AddConfigPath(home)
				}
			}

			if err := v.ReadInConfig(); err != nil {
				var notFound viper.ConfigFileNotFoundError
				if !errors.As(err, &notFound) {
					return err
				}
			}

			cfg := Config{
				Token:     v.GetString("token"),
				OrgGuid:   v.GetString("org-guid"),
				DenType:   v.GetString("den-type"),
				DenNumber: v.GetString("den-number"),
				Output:    v.GetString("output"),
			}
			if cfg.Output == "" {
				cfg.Output = fmt.Sprintf("%s-%s-progress.xlsx", cfg.DenType, cfg.DenNumber)
			}

			return runner(cmd.Context(), cfg)
		},
	}

	flags := cmd.Flags()
	flags.String("token", "", "Scouting.org API token")
	flags.String("org-guid", "", "Organization GUID")
	flags.String("den-type", "", "Den type (e.g. Wolf, Bear, Webelos)")
	flags.String("den-number", "", "Den number")
	flags.String("output", "", "Output XLSX path (default: {den-type}-{den-number}-progress.xlsx)")
	flags.StringVar(&configPath, "config", "", "Path to config file (default: ./scoutbook-xls.yaml or $HOME/scoutbook-xls.yaml)")

	for _, key := range []string{"token", "org-guid", "den-type", "den-number", "output"} {
		// BindPFlag cannot fail here because each flag was just registered above.
		_ = v.BindPFlag(key, flags.Lookup(key))
	}

	return cmd
}
