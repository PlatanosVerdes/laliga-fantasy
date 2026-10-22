// Package secret seals what the app keeps on disk that would open a session by itself: the
// LaLiga tokens and the Necroporra cookie. AES-256-GCM under one key per instance, so a copy of
// the data folder without the key opens nothing.
//
// The key is FANTASY_SECRET_KEY (32 bytes, base64 or hex) when set. Otherwise it is read from
// FANTASY_SECRET_FILE, by default <config root>/secret.key, created on first use.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

const version = 1

// ErrWrongKey is a file sealed under another key: the file is fine, the key is not.
var ErrWrongKey = errors.New("la clave no coincide: este fichero se cifro con otra " +
	"FANTASY_SECRET_KEY o con otro secret.key")

// ErrDamaged is a file sealed under this key that no longer authenticates.
var ErrDamaged = errors.New("el fichero cifrado esta danado o lo han modificado")

// envelope is what a sealed file holds. KeyID tells a wrong key from a damaged file.
type envelope struct {
	V     int    `json:"v"`
	KeyID string `json:"kid"`
	Nonce []byte `json:"nonce"`
	Data  []byte `json:"data"`
}

// Sealed is whether blob is an envelope rather than plaintext.
func Sealed(blob []byte) bool {
	var sealed envelope
	return json.Unmarshal(blob, &sealed) == nil && sealed.V > 0 && sealed.Data != nil
}

func Seal(plain []byte) ([]byte, error) {
	key, err := Key(true)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return json.Marshal(envelope{V: version, KeyID: keyID(key), Nonce: nonce,
		Data: aead.Seal(nil, nonce, plain, nil)})
}

func Open(blob []byte) ([]byte, error) {
	var sealed envelope
	if err := json.Unmarshal(blob, &sealed); err != nil || sealed.V == 0 {
		return nil, errors.New("no es un fichero cifrado")
	}
	if sealed.V != version {
		return nil, fmt.Errorf("formato cifrado v%d desconocido: actualiza la app", sealed.V)
	}
	// Never create a key here: a fresh one could only fail, and would hide the missing one.
	key, err := Key(false)
	if err != nil {
		return nil, err
	}
	if sealed.KeyID != keyID(key) {
		return nil, ErrWrongKey
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(sealed.Nonce) != aead.NonceSize() {
		return nil, ErrDamaged
	}
	plain, err := aead.Open(nil, sealed.Nonce, sealed.Data, nil)
	if err != nil {
		return nil, ErrDamaged
	}
	return plain, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func keyID(key []byte) string {
	sum := sha256.Sum256(append([]byte("laliga-fantasy key id\x00"), key...))
	return hex.EncodeToString(sum[:4])
}

// File is where the key is kept when FANTASY_SECRET_KEY is not set.
func File() string {
	if path := os.Getenv("FANTASY_SECRET_FILE"); path != "" {
		return path
	}
	return filepath.Join(config.ConfigRoot, "secret.key")
}

var keyMu sync.Mutex

// Key is the instance's key. With create, a missing key file is generated.
func Key(create bool) ([]byte, error) {
	if value := os.Getenv("FANTASY_SECRET_KEY"); value != "" {
		key, err := parseKey(value)
		if err != nil {
			return nil, fmt.Errorf("FANTASY_SECRET_KEY: %w", err)
		}
		return key, nil
	}
	keyMu.Lock()
	defer keyMu.Unlock()
	path := File()
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return nil, fmt.Errorf("falta la clave para descifrar: ni FANTASY_SECRET_KEY ni %s", path)
		}
		return generate(path)
	}
	if err != nil {
		return nil, err
	}
	key, err := parseKey(string(body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return key, nil
}

func parseKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if len(value) == 64 {
		if key, err := hex.DecodeString(value); err == nil {
			return key, nil
		}
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := encoding.DecodeString(value); err == nil && len(key) == 32 {
			return key, nil
		}
	}
	return nil, errors.New("la clave tiene que ser 32 bytes en base64 o en hex")
}

func generate(path string) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// O_EXCL, not an atomic rename: a rename would replace a key another process just made.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	_, err = file.WriteString(base64.StdEncoding.EncodeToString(key) + "\n")
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	slog.Info("secret key created", "file", path)
	return key, nil
}
