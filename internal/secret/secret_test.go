package secret

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func keyFile(t *testing.T) string {
	t.Helper()
	root := config.ConfigRoot
	t.Cleanup(func() { config.ConfigRoot = root })
	config.ConfigRoot = t.TempDir()
	t.Setenv("FANTASY_SECRET_KEY", "")
	t.Setenv("FANTASY_SECRET_FILE", "")
	return filepath.Join(config.ConfigRoot, "secret.key")
}

func TestSealOpensBackAndHidesThePlaintext(t *testing.T) {
	keyFile(t)
	plain := []byte(`{"refresh_token":"very-secret"}`)
	sealed, err := Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !Sealed(sealed) || Sealed(plain) {
		t.Fatal("sealed and plaintext told apart wrongly")
	}
	if bytes.Contains(sealed, []byte("very-secret")) {
		t.Fatalf("the plaintext shows in the sealed file: %s", sealed)
	}
	opened, err := Open(sealed)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatalf("opened %q, %v", opened, err)
	}
}

func TestATamperedCiphertextIsRefused(t *testing.T) {
	keyFile(t)
	sealed, err := Seal([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	var file envelope
	if err := json.Unmarshal(sealed, &file); err != nil {
		t.Fatal(err)
	}
	file.Data[0] ^= 1
	tampered, _ := json.Marshal(file)
	if _, err := Open(tampered); !errors.Is(err, ErrDamaged) {
		t.Errorf("a tampered file opened: %v", err)
	}
}

func TestAnotherKeyIsNamedAsTheCause(t *testing.T) {
	keyFile(t)
	sealed, err := Seal([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FANTASY_SECRET_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	_, err = Open(sealed)
	if !errors.Is(err, ErrWrongKey) || !strings.Contains(err.Error(), "la clave no coincide") {
		t.Errorf("wrong key answered %v", err)
	}
}

func TestTheEnvironmentKeyWinsOverTheFile(t *testing.T) {
	path := keyFile(t)
	if _, err := Key(true); err != nil {
		t.Fatal(err)
	}
	env := bytes.Repeat([]byte{9}, 32)
	t.Setenv("FANTASY_SECRET_KEY", hex.EncodeToString(env))
	key, err := Key(true)
	if err != nil || !bytes.Equal(key, env) {
		t.Fatalf("key %x, %v", key, err)
	}
	fromFile, _ := os.ReadFile(path)
	if decoded, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(string(fromFile))); bytes.Equal(decoded, env) {
		t.Error("the file holds the environment key")
	}
}

func TestAMissingKeyFileIsCreatedPrivate(t *testing.T) {
	path := keyFile(t)
	if _, err := Key(false); err == nil {
		t.Fatal("opening created a key")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a key file was created only to open")
	}
	first, err := Key(true)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file %v, %v", info, err)
	}
	if again, _ := Key(true); !bytes.Equal(first, again) {
		t.Error("the key changed between calls")
	}
}

func TestTheKeyFileCanBeElsewhere(t *testing.T) {
	keyFile(t)
	path := filepath.Join(t.TempDir(), "outside", "key")
	t.Setenv("FANTASY_SECRET_FILE", path)
	if _, err := Key(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("FANTASY_SECRET_FILE was ignored")
	}
}

func TestABadKeyIsRefused(t *testing.T) {
	keyFile(t)
	t.Setenv("FANTASY_SECRET_KEY", "too-short")
	if _, err := Seal([]byte("x")); err == nil {
		t.Error("sealed under a key that is not 32 bytes")
	}
}
