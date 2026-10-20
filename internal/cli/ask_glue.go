package cli

import "github.com/aaxis-em/xis/internal/store"

func (a *App) askFunc(st *store.Store, ca *changeAnalysis) func(string) (string, error) {
	return nil
}
