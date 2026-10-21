package cli

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/buildinfo"
	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/notes"
	"github.com/aaxis-em/xis/internal/report"
	"github.com/aaxis-em/xis/internal/store"
)

func init() {
	register(newReportCmd)
	register(newNoteCmd)
	register(newExportCmd)
}

func xisVersion() string {
	v, _, _ := buildinfo.Info()
	return v
}

func (a *App) writeOutput(path string, data []byte) error {
	if path == "" || path == "-" {
		_, err := a.Out.Write(data)
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return userErr(err.Error(), "", err)
	}
	a.printf("wrote %s\n", path)
	return nil
}

func reportInput(st *store.Store, ca *changeAnalysis, noteRef string) (report.Input, error) {
	res := ca.Result
	in := report.Input{Change: res.Change, Files: res.Files, Provenance: map[string][]model.HunkProvenance{}, ActorKind: res.ActorKind,
		Sessions: ca.Sessions, Flags: ca.Flags, Rationales: map[string]model.Rationale{}, Location: ca.location,
		XisVersion: xisVersion(), NoteRef: noteRef, GeneratedAt: time.Now()}
	for _, p := range res.Provenance {
		in.Provenance[p.HunkID] = append(in.Provenance[p.HunkID], p)
	}
	var err error
	if in.Hunks, err = st.ListHunks(res.Change.ID); err != nil {
		return in, err
	}
	if in.Units, err = st.ListReviewUnits(res.Change.ID); err != nil {
		return in, err
	}
	rats, err := st.ListRationales(res.Change.ID)
	if err != nil {
		return in, err
	}
	for _, r := range rats {
		in.Rationales[r.HunkID] = r
	}
	return in, nil
}

func noteRefFor(repo string) string {
	if raw, err := notes.Show(repo, "HEAD"); err == nil && len(raw) > 0 {
		head, _ := (&App{}).headShort(repo)
		return notes.Ref + " on " + head
	}
	return notes.Ref + " (no note on HEAD)"
}

func (a *App) headShort(repo string) (string, error) {
	return gitRun(repo, "rev-parse", "--short", "HEAD")
}

func newReportCmd() *cobra.Command {
	var cf changeFlags
	var md, full bool
	var out string
	cmd := &cobra.Command{
		Use:   "report [SESSION]",
		Short: "Write a PR-ready summary of a change in Markdown or JSON",
		Long: `Write a summary for a pull request: agents, intent, flags, review units, test runs and unverified claims.
The report contains no secrets and no transcript text beyond short quoted claims. It has about 150 lines; use --full for all items.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newApp(cmd)
			session := ""
			if len(args) == 1 {
				session = args[0]
			}
			opts, err := cf.options(session)
			if err != nil {
				return err
			}
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ca, err := a.analyzeChange(st, opts)
			if err != nil {
				return err
			}
			in, err := reportInput(st, ca, noteRefFor(ca.Engine.Repo.Top))
			if err != nil {
				return err
			}
			r := report.Build(in)
			if a.JSON && !md {
				b, _ := json.MarshalIndent(r, "", "  ")
				return a.writeOutput(out, append(b, '\n'))
			}
			return a.writeOutput(out, []byte(report.Markdown(r, full)))
		},
	}
	cf.add(cmd)
	cmd.Flags().BoolVar(&md, "md", false, "write Markdown (the default; use the global --json flag for JSON)")
	cmd.Flags().BoolVar(&full, "full", false, "list all items, not only the first ones")
	cmd.Flags().StringVarP(&out, "output", "o", "", "write to a file instead of stdout")
	return cmd
}

func buildNote(st *store.Store, ca *changeAnalysis) notes.Note {
	res := ca.Result
	n := notes.Note{XisVersion: xisVersion(), CreatedAt: time.Now().UTC().Format(time.RFC3339), Flags: notes.NoteFlags{Suppressed: []notes.Suppressed{}}, Hunks: []notes.NoteHunk{}, Sessions: []notes.NoteSession{}}
	for _, d := range ca.Sessions {
		s := d.Session
		models := s.Models
		if models == nil {
			models = []string{}
		}
		n.Sessions = append(n.Sessions, notes.NoteSession{ID: s.ID, Agent: s.Agent, AgentVersion: s.AgentVersion, Models: models, Fidelity: string(s.Fidelity)})
	}
	for _, h := range res.Hunks {
		prov := res.ProvenanceOf(h.ID)
		steps := 0
		for _, p := range prov {
			if p.StepID != model.Unattributed {
				steps++
			}
		}
		n.Hunks = append(n.Hunks, notes.NoteHunk{File: h.FilePath, NewStart: h.NewStart, NewLines: h.NewLines, ContentHash: h.ContentHash,
			Steps: steps, Contributor: attribution.Contributor(prov, res.ActorKind)})
	}
	for _, f := range ca.Flags {
		if f.Suppressed {
			n.Flags.Suppressed = append(n.Flags.Suppressed, notes.Suppressed{Rule: f.RuleID, Reason: f.SuppressReason})
			continue
		}
		switch f.Severity {
		case model.SevHigh:
			n.Flags.High++
		case model.SevMedium:
			n.Flags.Medium++
		case model.SevLow:
			n.Flags.Low++
		default:
			n.Flags.Info++
		}
	}
	units := res.Units
	if reviewed := reviewedChange(st, res.Change); reviewed != "" {
		if us, err := st.ListReviewUnits(reviewed); err == nil && len(us) > 0 {
			units = us
		}
	} else if us, err := st.ListReviewUnits(res.Change.ID); err == nil {
		units = us
	}
	n.Review.Units = len(units)
	for _, u := range units {
		if u.Status == "understood" {
			n.Review.Understood++
		}
	}
	return n
}

// reviewedChange finds the latest working-tree or index change of the same
// sessions, because the user reviews before the commit exists.
func reviewedChange(st *store.Store, c model.Change) string {
	if len(c.SessionIDs) == 0 {
		return ""
	}
	all, err := st.ListChanges(c.RepoID)
	if err != nil {
		return ""
	}
	want := strings.Join(c.SessionIDs, ",")
	for _, x := range all {
		if (x.TargetRef == "worktree" || x.TargetRef == "index") && strings.Join(x.SessionIDs, ",") == want {
			return x.ID
		}
	}
	return ""
}

func loadKey(p config.Paths) ([]byte, error) {
	key, _, err := notes.LoadOrCreateKey(p.Keys)
	if err != nil {
		return nil, userErr("signing key: "+err.Error(), "Run `xis init`.", err)
	}
	return key, nil
}

func newNoteCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "note", Short: "Write, show and verify the signed provenance note of a commit (refs/notes/xis)"}
	var commit string
	var quiet bool
	write := &cobra.Command{
		Use:   "write",
		Short: "Attach a signed provenance note to a commit (default HEAD)",
		Long: `Attach a signed note under refs/notes/xis. The note holds only hashes, counts and IDs: no transcript text, prompts or code.
To share notes, run: git push origin refs/notes/xis`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			if err := a.requireRepo(); err != nil {
				return err
			}
			sha, err := gitRun(a.RepoDir, "rev-parse", "--verify", commit+"^{commit}")
			if err != nil {
				return userErr("unknown commit "+commit, "", err)
			}
			parent, err := gitRun(a.RepoDir, "rev-parse", "--verify", "-q", sha+"^")
			if err != nil {
				return userErr("the commit has no parent", "xis notes need a commit with a parent.", err)
			}
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ca, err := a.analyzeChange(st, attribution.Options{Mode: attribution.ModeRange, Range: parent + ".." + sha})
			if err != nil {
				return err
			}
			key, err := loadKey(a.Paths)
			if err != nil {
				return err
			}
			raw, err := notes.Sign(buildNote(st, ca), key)
			if err != nil {
				return err
			}
			if err := notes.Write(a.RepoDir, sha, raw); err != nil {
				return userErr("git notes: "+err.Error(), "", err)
			}
			if !quiet {
				a.printf("wrote %s note for %s (%d sessions, %d hunks)\n", notes.Ref, sha[:12], len(ca.Sessions), len(ca.Result.Hunks))
			}
			return nil
		},
	}
	write.Flags().StringVar(&commit, "commit", "HEAD", "commit to attach the note to")
	write.Flags().BoolVar(&quiet, "quiet", false, "print nothing")

	var showCommit string
	show := &cobra.Command{
		Use:   "show",
		Short: "Show the note of a commit and its signature status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			if err := a.requireRepo(); err != nil {
				return err
			}
			raw, err := notes.Show(a.RepoDir, showCommit)
			if err != nil {
				return userErr(err.Error(), "Run `xis note write` first.", err)
			}
			if a.JSON {
				_, err := a.Out.Write(append(raw, '\n'))
				return err
			}
			var pretty map[string]any
			_ = json.Unmarshal(raw, &pretty)
			b, _ := json.MarshalIndent(pretty, "", "  ")
			a.printf("%s\n", b)
			a.printf("signature: %s\n", signatureStatus(a, raw))
			return nil
		},
	}
	show.Flags().StringVar(&showCommit, "commit", "HEAD", "commit")

	var verifyCommit string
	verify := &cobra.Command{
		Use:   "verify",
		Short: "Verify the signature of the note of a commit (exit code 1 if it fails)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			if err := a.requireRepo(); err != nil {
				return err
			}
			raw, err := notes.Show(a.RepoDir, verifyCommit)
			if err != nil {
				return userErr(err.Error(), "Run `xis note write` first.", err)
			}
			status := signatureStatus(a, raw)
			a.printf("%s\n", status)
			if !strings.HasPrefix(status, "valid") {
				return exitCode(1)
			}
			return nil
		},
	}
	verify.Flags().StringVar(&verifyCommit, "commit", "HEAD", "commit")
	cmd.AddCommand(write, show, verify)
	return cmd
}

func signatureStatus(a *App, raw []byte) string {
	var local []byte
	if notes.KeyExists(a.Paths.Keys) {
		if key, _, err := notes.LoadOrCreateKey(a.Paths.Keys); err == nil {
			local = notes.PublicKey(key)
		}
	}
	_, trusted, err := notes.Verify(raw, local)
	switch {
	case errors.Is(err, notes.ErrBadSignature):
		return "INVALID: the note was changed after signing"
	case err != nil:
		return "INVALID: " + err.Error()
	case trusted:
		return "valid (signed with the local key)"
	}
	return "valid (signed with a key that is not the local key)"
}

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "export", Short: "Export provenance in other formats"}
	var cf changeFlags
	var out string
	at := &cobra.Command{
		Use:   "agent-trace [SESSION]",
		Short: "Export the change as an Agent Trace v0.1 record (JSON)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newApp(cmd)
			session := ""
			if len(args) == 1 {
				session = args[0]
			}
			opts, err := cf.options(session)
			if err != nil {
				return err
			}
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ca, err := a.analyzeChange(st, opts)
			if err != nil {
				return err
			}
			res := ca.Result
			rev := ""
			if t := res.Change.TargetRef; t != "worktree" && t != "index" {
				rev = t
			}
			sessions := make([]model.Session, len(ca.Sessions))
			for i, d := range ca.Sessions {
				sessions[i] = d.Session
			}
			rec := notes.AgentTrace(notes.TraceInput{Revision: rev, Target: res.Change.TargetRef, Hunks: res.Hunks, RawPatches: res.RawPatches,
				LineSteps: res.LineSteps, StepSession: res.StepSession, ActorKind: res.ActorKind, Sessions: sessions, XisVersion: xisVersion(), Now: time.Now()})
			b, _ := json.MarshalIndent(rec, "", "  ")
			return a.writeOutput(out, append(b, '\n'))
		},
	}
	cf.add(at)
	at.Flags().StringVarP(&out, "output", "o", "", "write to a file instead of stdout")
	cmd.AddCommand(at)
	return cmd
}
