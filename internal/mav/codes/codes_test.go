package codes

import "testing"

func TestRegistryHasNoIDCollisions(t *testing.T) {
	seen := map[string]bool{}
	for key, code := range Registry {
		if key != code.ID {
			t.Errorf("registry key %q != code.ID %q", key, code.ID)
		}
		if seen[code.ID] {
			t.Errorf("duplicate code id %q", code.ID)
		}
		seen[code.ID] = true
		if code.Title == "" {
			t.Errorf("code %s has empty Title", code.ID)
		}
	}
}

func TestCodeFieldsOmitsEmpty(t *testing.T) {
	c := Code{ID: "x", Title: "Title", Remediation: ""}
	f := c.Fields()
	if _, ok := f["remediation"]; ok {
		t.Fatalf("expected remediation omitted when empty, got %v", f)
	}
	if f["title"] != "Title" {
		t.Fatalf("expected title=Title, got %v", f)
	}
}

func TestCodeFieldsAllFields(t *testing.T) {
	c := Code{
		ID:          "x",
		Title:       "T",
		Remediation: "R",
		Driver:      "baguette",
		Capability:  "tap",
	}
	f := c.Fields()
	for k, want := range map[string]string{
		"title":       "T",
		"remediation": "R",
		"driver":      "baguette",
		"capability":  "tap",
	} {
		if f[k] != want {
			t.Errorf("Fields()[%s]=%q want %q", k, f[k], want)
		}
	}
}
