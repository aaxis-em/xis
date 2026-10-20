package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/notes"
	"github.com/aaxis-em/xis/internal/store"
)

func init() { register(newInitCmd) }

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the xis directories, the config file and the signing key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			if err := a.Paths.Ensure(); err != nil {
				return userErr(err.Error(), "Check the permissions of "+a.Paths.DataDir+".", err)
			}
			if _, err := os.Stat(a.Paths.ConfigFile); os.IsNotExist(err) {
				if err := config.WriteFile(a.Paths.ConfigFile, []byte(config.DefaultTOML)); err != nil {
					return err
				}
				a.printf("created %s\n", a.Paths.ConfigFile)
			} else {
				a.printf("config  %s (kept)\n", a.Paths.ConfigFile)
			}
			db, err := store.Open(a.Paths.DB)
			if err != nil {
				return userErr("open database: "+err.Error(), "Run `xis doctor`.", err)
			}
			db.Close()
			a.printf("database %s\n", a.Paths.DB)
			if _, created, err := notes.LoadOrCreateKey(a.Paths.Keys); err != nil {
				return err
			} else if created {
				a.printf("created signing key in %s\n", a.Paths.Keys)
			}
			a.printf("\nNext steps:\n")
			a.printf("  1. In your repository, run: xis hook install --agent claude-code\n")
			a.printf("  2. Use Claude Code as usual.\n")
			a.printf("  3. Run: xis review\n")
			if a.Cfg.LLM.EffectiveProvider() == "none" {
				a.printf("\nLLM analysis is off. Set ANTHROPIC_API_KEY, or set llm.provider, to turn it on.\n")
			}
			return nil
		},
	}
}
