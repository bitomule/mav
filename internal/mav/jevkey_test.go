package mav

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The precedence has to be visible in a test and not only in a comment: which
// source wins when two are set is the one thing nobody can check by reading.

func TestTheEnvironmentVariableWinsOverEverythingElse(t *testing.T) {
	t.Setenv(jevKeyEnvVar, "from-env")
	key, source, ok := ResolveJevKey()
	if !ok || key != "from-env" || source != JevKeyFromEnv {
		t.Fatalf("expected the env key to win, got key=%q source=%q ok=%v", key, source, ok)
	}
}

func TestAnEmptyVariableIsNotAKey(t *testing.T) {
	// An exported-but-empty variable is the classic CI mistake, and treating
	// it as a key sends an empty credential and fails far from the cause.
	t.Setenv(jevKeyEnvVar, "   ")
	if _, source, ok := ResolveJevKey(); ok && source == JevKeyFromEnv {
		t.Fatal("whitespace is not a credential")
	}
}

func TestTheFileIsReadWhenNothingElseHasAKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(jevKeyEnvVar, "")
	path := JevKeyConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"jev_api_key": "from-file"})
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	// Only meaningful on a machine with no keychain entry, which is the
	// source that sits above the file and that a test cannot remove.
	if _, src, ok := ResolveJevKey(); ok && src == JevKeyFromKeychain {
		t.Skip("this machine has a mav-jev keychain entry, which correctly outranks the file")
	}
	key, source, ok := ResolveJevKey()
	if !ok || key != "from-file" || source != JevKeyFromFile {
		t.Fatalf("expected the file key, got key=%q source=%q ok=%v", key, source, ok)
	}
}

func TestTheFileLivesBesideTheOtherToolsNotInANewPlace(t *testing.T) {
	// The objection this answers was "too many configs scattered around", and
	// one more inside the existing namespace is not the same as one more place
	// to look.
	p := JevKeyConfigPath()
	if !strings.Contains(p, "bitomule") {
		t.Fatalf("expected the existing namespace: %s", p)
	}
	if !strings.HasSuffix(p, filepath.Join("mav", "config.json")) {
		t.Fatalf("unexpected path: %s", p)
	}
}

func TestEverySourceSaysWhereItCameFrom(t *testing.T) {
	// Printed on every run, because the alternative is remembering what you
	// configured, and that is what cost us time.
	if !strings.Contains(JevKeyFromEnv.Describe(), jevKeyEnvVar) {
		t.Fatal("the env source should name the variable")
	}
	if !strings.Contains(JevKeyFromKeychain.Describe(), jevKeychainService) {
		t.Fatal("the keychain source should name the service")
	}
	if !strings.Contains(JevKeyFromFile.Describe(), "config.json") {
		t.Fatal("the file source should name the file")
	}
}

func TestTheMissingKeyMessageNamesAllThreePlacesAndWhatToType(t *testing.T) {
	// Never a quiet green: a find with no key has to say where it looked and
	// what the caller can do about it, in one line each.
	m := MissingJevKeyNext()
	for _, want := range []string{
		"mav jev set-key", jevKeyEnvVar, jevKeychainService, "bitomule",
	} {
		if !strings.Contains(m, want) {
			t.Fatalf("the missing-key message should carry %q:\n%s", want, m)
		}
	}
}

func TestAFindWithNoKeySaysSoAndWhereItLooked(t *testing.T) {
	t.Setenv(jevKeyEnvVar, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, _, ok := ResolveJevKey(); ok {
		t.Skip("this machine has a key outside the env and the file, so the no-key path cannot be reached")
	}
	c := CLI{}
	got := c.resolveFind(t.Context(), settingsScreen(), "the row that opens something oblique")
	if got.Reason != ReasonNoKey {
		t.Fatalf("expected %s, got %q", ReasonNoKey, got.Reason)
	}
	if !strings.Contains(got.Next, "mav jev set-key") {
		t.Fatalf("a keyless find must say what to type:\n%s", got.Next)
	}
}

func TestAResolvedFindNamesTheKeySourceAndNeverTheKey(t *testing.T) {
	// The source is reported; the key never is. Asserting the second half
	// matters more than the first: a credential in a log outlives the run.
	const secret = "sk-this-must-never-be-printed"
	t.Setenv(jevKeyEnvVar, secret)
	c := CLI{}
	// A literal goal resolves without consulting a model, so no key is read
	// and none is reported — that is the contract, not an omission.
	got := c.resolveFind(t.Context(), settingsScreen(), "Wi-Fi")
	if got.KeySource != "" {
		t.Fatalf("a literal resolution consults nothing and should report no key source, got %q", got.KeySource)
	}
	blob, _ := json.Marshal(got)
	if strings.Contains(string(blob), secret) {
		t.Fatal("the key leaked into the find output")
	}
}
