package notes

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaxis-em/xis/internal/config"
)

func keyPaths(keysDir string) (priv, pub string) {
	return filepath.Join(keysDir, "ed25519"), filepath.Join(keysDir, "ed25519.pub")
}

func LoadOrCreateKey(keysDir string) (ed25519.PrivateKey, bool, error) {
	privPath, pubPath := keyPaths(keysDir)
	if b, err := os.ReadFile(privPath); err == nil {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(raw) != ed25519.PrivateKeySize {
			return nil, false, fmt.Errorf("signing key %s is not valid", privPath)
		}
		return ed25519.PrivateKey(raw), false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, false, err
	}
	if err := config.WriteFile(privPath, []byte(base64.StdEncoding.EncodeToString(priv)+"\n")); err != nil {
		return nil, false, err
	}
	if err := config.WriteFile(pubPath, []byte(base64.StdEncoding.EncodeToString(pub)+"\n")); err != nil {
		return nil, false, err
	}
	return priv, true, nil
}

func KeyExists(keysDir string) bool {
	priv, _ := keyPaths(keysDir)
	_, err := os.Stat(priv)
	return err == nil
}
