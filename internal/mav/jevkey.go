package mav

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Where mav's jev API key comes from, and in what order. The order and the
// reasoning are musts' (crates/musts/src/bin/musts-jev/jevkey.rs); this is the
// same shape in Go so that pointing both tools at one shared secret later is a
// change to two constants and nothing else.
//
//  1. MAV_JEV_API_KEY in the environment. For a caller that owns the secret
//     already. Not the interactive path: an environment variable shows up in
//     process listings and is inherited by every child.
//  2. The system keychain, through /usr/bin/security. The keychain ACL is per
//     accessing binary, and the accessing binary is always `security`, so
//     rebuilding or reinstalling mav never starts prompting.
//  3. ~/.config/bitomule/mav/config.json, mode 0600, beside the other tools
//     already in that namespace rather than in a new top-level directory.
//  4. Nothing, and `find` says so in its output instead of looking like a
//     search that found nothing.
//
// Deliberately absent: musts' key and jevi's key are never read. Inheriting
// another tool's credential silently is how "it works on my machine and I do
// not know why" starts.
const (
	jevKeyEnvVar        = "MAV_JEV_API_KEY"
	jevKeychainService  = "mav-jev"
	jevKeychainAccount  = "mav"
	jevProviderEnvVar   = "OPENROUTER_API_KEY"
	jevKeyConfigRelPath = "bitomule/mav/config.json"
)

// JevKeySource names where a key came from, so a run can say it rather than
// leaving it to be remembered.
type JevKeySource string

const (
	JevKeyFromEnv      JevKeySource = "env"
	JevKeyFromKeychain JevKeySource = "keychain"
	JevKeyFromFile     JevKeySource = "file"
)

// Describe renders the source the way it is printed to a person.
func (s JevKeySource) Describe() string {
	switch s {
	case JevKeyFromEnv:
		return "the " + jevKeyEnvVar + " environment variable"
	case JevKeyFromKeychain:
		return "the system keychain (service `" + jevKeychainService + "`)"
	case JevKeyFromFile:
		return JevKeyConfigPath()
	}
	return string(s)
}

// JevKeyConfigPath is the file that holds the key when there is no keychain.
func JevKeyConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if home := os.Getenv("HOME"); home != "" {
			base = filepath.Join(home, ".config")
		} else {
			base = ".config"
		}
	}
	return filepath.Join(base, filepath.FromSlash(jevKeyConfigRelPath))
}

// ResolveJevKey returns the key and where it came from, or false.
func ResolveJevKey() (string, JevKeySource, bool) {
	if v := strings.TrimSpace(os.Getenv(jevKeyEnvVar)); v != "" {
		return v, JevKeyFromEnv, true
	}
	if v, ok := jevKeyFromKeychain(); ok {
		return v, JevKeyFromKeychain, true
	}
	if v, ok := jevKeyFromFile(); ok {
		return v, JevKeyFromFile, true
	}
	return "", "", false
}

func jevKeyFromKeychain() (string, bool) {
	out, err := exec.Command("/usr/bin/security",
		"find-generic-password", "-s", jevKeychainService, "-w").Output()
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(out))
	return v, v != ""
}

func jevKeyFromFile() (string, bool) {
	raw, err := os.ReadFile(JevKeyConfigPath())
	if err != nil {
		return "", false
	}
	var doc struct {
		JevAPIKey string `json:"jev_api_key"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", false
	}
	v := strings.TrimSpace(doc.JevAPIKey)
	return v, v != ""
}

// MissingJevKeyNext is the `next` field of a find that had no key: every line
// is something the caller can act on.
func MissingJevKeyNext() string {
	return fmt.Sprintf("set one with `mav jev set-key < key.txt`, or export %s. Looked in, in order: %s, the system keychain (service `%s`), then %s",
		jevKeyEnvVar, jevKeyEnvVar, jevKeychainService, JevKeyConfigPath())
}

// StoreJevKey writes a key, keychain first, and says where it went. Reads from
// the caller rather than an argument so the secret never reaches shell history.
func StoreJevKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("no key on stdin")
	}
	err := exec.Command("/usr/bin/security", "add-generic-password",
		"-a", jevKeychainAccount, "-s", jevKeychainService, "-w", key, "-U").Run()
	if err == nil {
		return JevKeyFromKeychain.Describe(), nil
	}

	path := JevKeyConfigPath()
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("cannot create %s: %w", dir, err)
		}
	}
	body, _ := json.Marshal(map[string]string{"jev_api_key": key})
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("cannot write %s: %w", path, err)
	}
	return path + " (mode 0600)", nil
}
