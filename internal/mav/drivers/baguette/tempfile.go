package baguette

import (
	"os"
)

// writeTemp writes body to a fresh file in os.TempDir() and returns the path
// plus a cleanup function. Caller must defer cleanup(). Used by W3CActions to
// hand a JSON file path to the baguette subprocess.
func writeTemp(body []byte) (string, func(), error) {
	f, err := os.CreateTemp("", "mav-baguette-actions-*.json")
	if err != nil {
		return "", func() {}, err
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", func() {}, err
	}
	return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
}
