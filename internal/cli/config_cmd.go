package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/config"
)

func init() { register(newConfigCmd) }

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show and change the xis config",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Show the paths of the config files and the data directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			repoFile := ""
			if a.RepoDir != "" {
				repoFile = filepath.Join(a.RepoDir, ".xis.toml")
			}
			if a.JSON {
				return json.NewEncoder(a.Out).Encode(map[string]string{"config": a.Paths.ConfigFile, "repo_config": repoFile, "data": a.Paths.DataDir})
			}
			a.printf("config:      %s\n", a.Paths.ConfigFile)
			if repoFile != "" {
				a.printf("repo config: %s\n", repoFile)
			}
			a.printf("data:        %s\n", a.Paths.DataDir)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "get [KEY]",
		Short: "Show the value of one key, or of all keys",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newApp(cmd)
			if err := a.requireConfig(); err != nil {
				return err
			}
			keys := config.Keys()
			if len(args) == 1 {
				keys = args
			}
			out := map[string]string{}
			for _, k := range keys {
				v, err := a.Cfg.Get(k)
				if err != nil {
					return userErr(err.Error(), "Run `xis config get` to see all keys.", err)
				}
				out[k] = v
			}
			if a.JSON {
				return json.NewEncoder(a.Out).Encode(out)
			}
			if len(args) == 1 {
				a.printf("%s\n", out[args[0]])
				return nil
			}
			for _, k := range keys {
				a.printf("%s = %s\n", k, out[k])
			}
			return nil
		},
	})
	var repoScope bool
	set := &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Set one key in the user config file (or in .xis.toml with --repo-file)",
		Long: `Set one key. Use a comma to separate list items, and key=value for map items.
The command keeps the other keys in the file. It removes comments from the file.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newApp(cmd)
			path := a.Paths.ConfigFile
			if repoScope {
				if err := a.requireRepo(); err != nil {
					return err
				}
				path = filepath.Join(a.RepoDir, ".xis.toml")
			}
			old, readErr := os.ReadFile(path)
			if err := config.SetInFile(path, args[0], args[1]); err != nil {
				return userErr(err.Error(), "Run `xis config get` to see all keys.", err)
			}
			if c, err := config.Load(a.Paths, a.RepoDir); err == nil {
				err = c.Validate()
				if err != nil {
					if readErr == nil {
						_ = config.WriteFile(path, old)
					} else {
						_ = os.Remove(path)
					}
					return userErr(err.Error(), "The file is unchanged.", err)
				}
			}
			fmt.Fprintf(a.Out, "set %s = %s in %s\n", args[0], args[1], path)
			return nil
		},
	}
	set.Flags().BoolVar(&repoScope, "repo-file", false, "write to <repo>/.xis.toml instead of the user config file")
	cmd.AddCommand(set)
	return cmd
}
