package attribution

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aaxis-em/xis/internal/model"
)

var typeOrder = map[model.ChangeType]int{
	model.ChangeBehavior:   0,
	model.ChangeUnknown:    1,
	model.ChangeConfig:     2,
	model.ChangeDependency: 3,
	model.ChangeTest:       4,
	model.ChangeRefactor:   5,
	model.ChangeDocs:       6,
	model.ChangeGenerated:  7,
}

var typeTitle = map[model.ChangeType]string{
	model.ChangeBehavior:   "Behavior",
	model.ChangeUnknown:    "Other",
	model.ChangeConfig:     "Config",
	model.ChangeDependency: "Dependencies",
	model.ChangeTest:       "Tests",
	model.ChangeRefactor:   "Refactor",
	model.ChangeDocs:       "Docs",
	model.ChangeGenerated:  "Generated",
}

func topDir(path string) string {
	if i := strings.Index(path, "/"); i >= 0 {
		return path[:i]
	}
	return "."
}

func UnitID(changeID, key string) string {
	return model.StableID(zeroTime, changeID, "unit", key)
}

// GroupDeterministic groups hunks by change type, then by top-level
// directory, and orders the units behavior → config/dependency → tests →
// refactor → docs → generated. It sets ReviewUnitID on every hunk.
func GroupDeterministic(changeID string, hunks []model.Hunk) []model.ReviewUnit {
	type key struct {
		ct  model.ChangeType
		dir string
	}
	groups := map[key][]int{}
	for i, h := range hunks {
		k := key{h.ChangeType, topDir(h.FilePath)}
		groups[k] = append(groups[k], i)
	}
	keys := make([]key, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if typeOrder[keys[i].ct] != typeOrder[keys[j].ct] {
			return typeOrder[keys[i].ct] < typeOrder[keys[j].ct]
		}
		return keys[i].dir < keys[j].dir
	})
	units := make([]model.ReviewUnit, 0, len(keys))
	for n, k := range keys {
		idx := groups[k]
		sort.SliceStable(idx, func(a, b int) bool {
			ha, hb := hunks[idx[a]], hunks[idx[b]]
			if ha.FilePath != hb.FilePath {
				return ha.FilePath < hb.FilePath
			}
			return ha.NewStart < hb.NewStart
		})
		files := map[string]bool{}
		for _, i := range idx {
			files[hunks[i].FilePath] = true
		}
		title := typeTitle[k.ct]
		switch {
		case len(files) == 1:
			title += ": " + hunks[idx[0]].FilePath
		case k.dir != ".":
			title += ": " + k.dir + "/"
		default:
			title += fmt.Sprintf(": %d files", len(files))
		}
		u := model.ReviewUnit{ID: UnitID(changeID, "rule:"+string(k.ct)+":"+k.dir), ChangeID: changeID, Title: title, Ord: n + 1, Source: "rule"}
		for _, i := range idx {
			hunks[i].ReviewUnitID = u.ID
		}
		units = append(units, u)
	}
	return units
}

func Contributor(prov []model.HunkProvenance, actorKind map[string]string) string {
	var ai, human, unknown int
	for _, p := range prov {
		switch {
		case p.StepID == model.Unattributed:
			unknown += p.Lines
		case actorKind[p.StepID] == "human":
			human += p.Lines
		default:
			ai += p.Lines
		}
	}
	switch {
	case ai == 0 && human == 0:
		return "unknown"
	case human == 0 && unknown == 0:
		return "ai"
	case ai == 0 && unknown == 0:
		return "human"
	}
	return "mixed"
}
