package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/pkaeding/scoutbook-xls/internal/blackpug"
	"github.com/pkaeding/scoutbook-xls/internal/scouting"
)

// ImportConfig holds the resolved settings for the import-blackpug command.
type ImportConfig struct {
	Token        string
	TroopOrgGUID string
	CSVPath      string
	Apply        bool
	// Badges limits the import to these badge names. Empty means all.
	Badges  []string
	BaseURL string // tests only; empty means production
	Out     io.Writer
	// Now supplies "today" for the entered/approved dates. Nil means time.Now.
	Now func() time.Time
}

// ImportRunnerFunc is what the import-blackpug command invokes once its
// configuration is resolved.
type ImportRunnerFunc func(ctx context.Context, cfg ImportConfig) error

func newImportCmd(runner ImportRunnerFunc) *cobra.Command {
	v := viper.New()
	v.SetEnvPrefix("SCOUTBOOK")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	var configPath string
	cmd := &cobra.Command{
		Use:   "import-blackpug <export.csv>",
		Short: "Record merit badge requirements from a Black Pug export in Scoutbook",
		Long: `Reads a Black Pug (summer camp) CSV export and records each completed merit
badge requirement on Scouting.org, approved by you, dated as in the export.

By default this is a dry run: it reads from Scouting.org and reports what it
would record, but changes nothing. Pass --apply to record it. Requirements
already on record are never overwritten. If any requirement number in the
export can't be matched to the badge's catalog, nothing is recorded for that
scout and badge, and the unmatched numbers are listed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := readConfigFile(v, configPath); err != nil {
				return err
			}
			warnRenamedOrgGUID(v)
			cmd.SilenceUsage = true // from here on, errors aren't usage mistakes

			// --apply is read straight from the flag, not viper: a stray
			// SCOUTBOOK_APPLY or "apply: true" in a config file must not be
			// able to turn a dry run into a write.
			badges, err := cmd.Flags().GetStringSlice("badge")
			if err != nil {
				return err
			}
			apply, err := cmd.Flags().GetBool("apply")
			if err != nil {
				return err
			}
			return runner(cmd.Context(), ImportConfig{
				Token:        v.GetString("token"),
				TroopOrgGUID: v.GetString("troop-org-guid"),
				CSVPath:      args[0],
				Apply:        apply,
				Badges:       badges,
				Out:          cmd.OutOrStdout(),
			})
		},
	}
	flags := cmd.Flags()
	flags.String("token", "", "Scouting.org API token")
	flags.String("troop-org-guid", "", "Organization GUID of the troop to record advancement in (required)")
	flags.StringSlice("badge", nil, "Only import this merit badge (repeatable or comma-separated), e.g. --badge Leatherwork")
	flags.Bool("apply", false, "Record the advancement (default is a dry run)")
	flags.StringVar(&configPath, "config", "", "Path to config file (default: ./scoutbook-xls.yaml or $HOME/scoutbook-xls.yaml)")
	for _, key := range []string{"token", "troop-org-guid"} {
		_ = v.BindPFlag(key, flags.Lookup(key))
	}
	return cmd
}

// runImport is the default ImportRunnerFunc.
func runImport(ctx context.Context, cfg ImportConfig) error {
	if cfg.Token == "" {
		return errors.New("token is required (set --token, SCOUTBOOK_TOKEN, or token: in config)")
	}
	if cfg.TroopOrgGUID == "" {
		// Discovery only knows how to find a Pack; a camp export is a Troop's.
		return errors.New("troop-org-guid is required: the troop's organization GUID (set --troop-org-guid, SCOUTBOOK_TROOP_ORG_GUID, or troop-org-guid: in config)")
	}
	claims, err := scouting.ParseJWT(cfg.Token)
	if err != nil {
		return fmt.Errorf("parse token: %w", err)
	}
	if claims.UID == 0 {
		return errors.New("token has no uid claim, so the approving leader can't be determined")
	}

	f, err := os.Open(cfg.CSVPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	rows, err := blackpug.ParseCSV(f)
	if err != nil {
		return err
	}

	rows, err = blackpug.FilterBadges(rows, cfg.Badges)
	if err != nil {
		return err
	}

	out := cfg.Out
	if out == nil {
		out = os.Stdout
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := scouting.NewClient(baseURL, cfg.Token)

	report, err := blackpug.Import(ctx, client, rows, blackpug.Options{
		OrgGUID:      cfg.TroopOrgGUID,
		LeaderUserID: claims.UID,
		Today:        now(),
		Apply:        cfg.Apply,
	})
	report.Write(out, cfg.Apply)
	if err != nil {
		return handleTokenExpired(err)
	}
	if report.Problems() {
		return errors.New("some merit badges were not recorded; see above")
	}
	return nil
}
