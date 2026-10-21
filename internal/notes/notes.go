package notes

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/gitx"
)

const Ref = "refs/notes/xis"

type NoteSession struct {
	ID           string   `json:"id"`
	Agent        string   `json:"agent"`
	AgentVersion string   `json:"agent_version"`
	Models       []string `json:"models"`
	Fidelity     string   `json:"fidelity"`
}

type NoteHunk struct {
	File        string `json:"file"`
	NewStart    int    `json:"new_start"`
	NewLines    int    `json:"new_lines"`
	ContentHash string `json:"content_hash"`
	Steps       int    `json:"steps"`
	Contributor string `json:"contributor"`
}

type Suppressed struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}

type NoteFlags struct {
	High       int          `json:"high"`
	Medium     int          `json:"medium"`
	Low        int          `json:"low"`
	Info       int          `json:"info"`
	Suppressed []Suppressed `json:"suppressed"`
}

type NoteReview struct {
	Units      int `json:"units"`
	Understood int `json:"understood"`
}

// Note holds only hashes, counts and IDs: never transcript text, prompts or code.
type Note struct {
	V          int           `json:"v"`
	Sessions   []NoteSession `json:"sessions"`
	Hunks      []NoteHunk    `json:"hunks"`
	Flags      NoteFlags     `json:"flags"`
	Review     NoteReview    `json:"review"`
	XisVersion string        `json:"xis_version"`
	CreatedAt  string        `json:"created_at"`
	Sig        string        `json:"sig,omitempty"`
	PubKey     string        `json:"pubkey"`
}

// canonical returns the JSON with sorted keys and without "sig". Numbers stay
// as written, so a note verifies after any JSON round-trip.
func canonical(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	delete(m, "sig")
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

func Sign(n Note, key ed25519.PrivateKey) ([]byte, error) {
	if n.CreatedAt == "" {
		n.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	n.V = 1
	n.Sig = ""
	n.PubKey = base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))
	raw, err := json.Marshal(n)
	if err != nil {
		return nil, err
	}
	msg, err := canonical(raw)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(key, msg)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(msg))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	m["sig"] = base64.StdEncoding.EncodeToString(sig)
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

var ErrBadSignature = errors.New("signature does not match the note")

// Verify checks the signature with the public key in the note. trusted is
// true when that key is the local signing key.
func Verify(raw []byte, localPub ed25519.PublicKey) (n Note, trusted bool, err error) {
	if err := json.Unmarshal(raw, &n); err != nil {
		return n, false, fmt.Errorf("note is not valid JSON: %w", err)
	}
	pub, err := base64.StdEncoding.DecodeString(n.PubKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return n, false, errors.New("note has no valid public key")
	}
	sig, err := base64.StdEncoding.DecodeString(n.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return n, false, errors.New("note has no valid signature")
	}
	msg, err := canonical(raw)
	if err != nil {
		return n, false, err
	}
	if !ed25519.Verify(pub, msg, sig) {
		return n, false, ErrBadSignature
	}
	return n, localPub != nil && bytes.Equal(pub, localPub), nil
}

func gitIdentityEnv(repo string) []string {
	if _, err := gitx.In(repo).Run("var", "GIT_COMMITTER_IDENT"); err == nil {
		return nil
	}
	return []string{"GIT_AUTHOR_NAME=xis", "GIT_AUTHOR_EMAIL=xis@localhost", "GIT_COMMITTER_NAME=xis", "GIT_COMMITTER_EMAIL=xis@localhost"}
}

func Write(repo, commit string, note []byte) error {
	_, err := gitx.In(repo).With(gitIdentityEnv(repo)...).WithStdin(bytes.NewReader(note)).
		Run("notes", "--ref", Ref, "add", "-f", "-F", "-", commit)
	return err
}

func Show(repo, commit string) ([]byte, error) {
	out, err := gitx.In(repo).Output("notes", "--ref", Ref, "show", commit)
	if err != nil {
		if strings.Contains(err.Error(), "no note found") {
			return nil, fmt.Errorf("commit %s has no xis note", commit)
		}
		return nil, err
	}
	return bytes.TrimSpace(out), nil
}

func PublicKey(key ed25519.PrivateKey) ed25519.PublicKey {
	return key.Public().(ed25519.PublicKey)
}
