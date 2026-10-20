package cli

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/buildinfo"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the version, commit and build date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			v, c, d := buildinfo.Info()
			out := cmd.OutOrStdout()
			if g.json {
				return json.NewEncoder(out).Encode(map[string]string{
					"version": v, "commit": c, "date": d, "go": runtime.Version(),
					"platform": runtime.GOOS + "/" + runtime.GOARCH,
				})
			}
			fmt.Fprintf(out, "xis %s\ncommit: %s\nbuilt: %s\ngo: %s %s/%s\n", v, c, d, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
