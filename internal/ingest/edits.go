package ingest

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/aaxis-em/xis/internal/gitx"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/snapshot"
)

func (in *Ingester) deriveEdits(sh *snapshot.Shadow, tr *model.Trace) error {
	marks := tr.Snapshots
	if len(marks) < 2 {
		return nil
	}
	args := []string{"rev-parse"}
	for _, m := range marks {
		args = append(args, m.Commit+"^{tree}")
	}
	out, err := sh.Git().Run(args...)
	if err != nil {
		return err
	}
	trees := strings.Split(out, "\n")
	if len(trees) != len(marks) {
		return fmt.Errorf("rev-parse returned %d trees for %d snapshots", len(trees), len(marks))
	}
	steps := map[string]model.Step{}
	for _, s := range tr.Steps {
		steps[s.ID] = s
	}
	for i := 1; i < len(marks); i++ {
		cur := marks[i]
		if trees[i] == trees[i-1] || cur.StepID == "" {
			continue
		}
		edits, err := in.diffEdits(sh, marks[i-1].Commit, cur.Commit, steps[cur.StepID])
		if err != nil {
			return err
		}
		tr.Edits = append(tr.Edits, edits...)
	}
	linkReverts(tr.Edits)
	return nil
}

func (in *Ingester) diffEdits(sh *snapshot.Shadow, from, to string, step model.Step) ([]model.Edit, error) {
	raw, err := sh.Git().Output("diff", "--raw", "-z", "-M", "--no-abbrev", from, to)
	if err != nil {
		return nil, err
	}
	changes, err := gitx.ParseRaw(raw)
	if err != nil {
		return nil, err
	}
	patchOut, err := sh.Git().Output("diff", "-M", "-U3", "--no-color", "--no-ext-diff", from, to)
	if err != nil {
		return nil, err
	}
	patches, err := gitx.ParsePatch(patchOut)
	if err != nil {
		return nil, err
	}
	byPath := map[string]gitx.FilePatch{}
	for _, p := range patches {
		byPath[p.Path] = p
	}
	sizes := blobSizes(sh, changes)
	var edits []model.Edit
	for _, c := range changes {
		p := byPath[c.Path]
		e := model.Edit{
			ID:         model.StableID(step.TsStart, step.ID, c.Path),
			StepID:     step.ID,
			FilePath:   c.Path,
			OldPath:    c.OldPath,
			BeforeBlob: c.OldBlob,
			AfterBlob:  c.NewBlob,
			Added:      p.Added,
			Removed:    p.Removed,
		}
		switch {
		case p.Binary:
			e.Patch = "[binary file]"
		case sizes[c.NewBlob] > in.Cfg.Capture.MaxFileBytes || sizes[c.OldBlob] > in.Cfg.Capture.MaxFileBytes:
			e.Patch = fmt.Sprintf("[large file: %d bytes]", max(sizes[c.NewBlob], sizes[c.OldBlob]))
		default:
			e.Patch = in.redactor.String(p.Text)
		}
		edits = append(edits, e)
	}
	return edits, nil
}

func blobSizes(sh *snapshot.Shadow, changes []gitx.RawChange) map[string]int64 {
	var in bytes.Buffer
	for _, c := range changes {
		for _, b := range []string{c.OldBlob, c.NewBlob} {
			if b != "" {
				in.WriteString(b + "\n")
			}
		}
	}
	out := map[string]int64{}
	if in.Len() == 0 {
		return out
	}
	res, err := sh.Git().WithStdin(&in).Output("cat-file", "--batch-check=%(objectname) %(objectsize)")
	if err != nil {
		return out
	}
	sc := bufio.NewScanner(bytes.NewReader(res))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 {
			n, _ := strconv.ParseInt(f[1], 10, 64)
			out[f[0]] = n
		}
	}
	return out
}

func linkReverts(edits []model.Edit) {
	for i := range edits {
		if edits[i].BeforeBlob == "" && edits[i].AfterBlob == "" {
			continue
		}
		for j := i + 1; j < len(edits); j++ {
			if edits[j].FilePath == edits[i].FilePath && edits[j].AfterBlob == edits[i].BeforeBlob && edits[j].StepID != edits[i].StepID {
				edits[i].RevertedByEditID = edits[j].ID
				break
			}
		}
	}
}
