package mav

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func TestAutomationStringExpansionPreservesNestedTypesAndLiterals(t *testing.T) {
	selector := &drivers.ElementSelector{Text: "${env.label}"}
	original := Automation{Command: "batch", Options: map[string]any{
		"Text": "${env.label}", "Count": int64(4), "Scale": 0.5, "Enabled": true, "Nothing": nil,
		"Selector": selector,
		"Nested":   []any{map[string]any{"${env.key}": "${env.label}"}, []string{"literal", "${env.label}"}},
		"Typed":    map[string]string{"label": "${env.label}"},
	}}
	actual, err := transformAutomationStrings(original, func(value string) (string, error) {
		return expandEnvString(value, map[string]string{"label": "Continue"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"Text": "Continue", "Count": int64(4), "Scale": 0.5, "Enabled": true, "Nothing": nil} {
		if !reflect.DeepEqual(actual.Options[key], want) {
			t.Fatalf("%s=%#v want %#v", key, actual.Options[key], want)
		}
	}
	if actual.Options["Selector"].(*drivers.ElementSelector).Text != "Continue" {
		t.Fatalf("selector=%#v", actual.Options["Selector"])
	}
	nested := actual.Options["Nested"].([]any)
	if nested[0].(map[string]any)["${env.key}"] != "Continue" || !reflect.DeepEqual(nested[1], []string{"literal", "Continue"}) {
		t.Fatalf("nested=%#v", nested)
	}
	actual.Options["Typed"].(map[string]string)["label"] = "changed"
	actual.Options["Selector"].(*drivers.ElementSelector).Text = "changed"
	nested[1].([]string)[0] = "changed"
	if selector.Text != "${env.label}" || original.Options["Typed"].(map[string]string)["label"] != "${env.label}" || original.Options["Nested"].([]any)[1].([]string)[0] != "literal" {
		t.Fatalf("original mutated: %#v", original.Options)
	}
}

func TestAutomationBindingsAreFreshAcrossRetries(t *testing.T) {
	original := Automation{Command: "type", Options: map[string]any{
		"Text": "${env.prefix} ${exec.account.id}", "Selector": map[string]any{"Text": "${env.field}"},
	}}
	for _, id := range []string{"first", "second"} {
		prepared, err := transformAutomationStrings(original, func(value string) (string, error) {
			return expandEnvString(value, map[string]string{"prefix": "Account", "field": "Name"})
		})
		if err != nil {
			t.Fatal(err)
		}
		prepared, err = transformAutomationStrings(prepared, func(value string) (string, error) {
			return substituteExecBindings(value, flowExecBindings{"account": newFlowExecBinding(`{"id":"` + id + `"}`)})
		})
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Options["Text"] != "Account "+id {
			t.Fatalf("retry %s options=%#v", id, prepared.Options)
		}
		if err := validateAutomation(prepared); err != nil {
			t.Fatal(err)
		}
		prepared.Options["Selector"].(map[string]any)["Text"] = "mutated attempt"
	}
	if original.Options["Text"] != "${env.prefix} ${exec.account.id}" || original.Options["Selector"].(map[string]any)["Text"] != "${env.field}" {
		t.Fatalf("original mutated: %#v", original.Options)
	}
}

func TestAutomationExpansionFailsWithoutMutatingSharedOptions(t *testing.T) {
	failure := errors.New("binding unavailable")
	original := Automation{Command: "type", Options: map[string]any{"Text": []string{"first", "fail"}}}
	_, err := transformAutomationStrings(original, func(value string) (string, error) {
		if value == "fail" {
			return "", failure
		}
		return "changed", nil
	})
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "automation options") {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(original.Options["Text"], []string{"first", "fail"}) {
		t.Fatalf("original mutated: %#v", original.Options)
	}
}

func TestAutomationNumericTemplatesRemainInvalidBeforeDispatch(t *testing.T) {
	req := Automation{Command: "tap", Options: map[string]any{"X": "${env.x}", "Y": 20}}
	prepared, err := transformAutomationStrings(req, func(value string) (string, error) { return expandEnvString(value, map[string]string{"x": "12"}) })
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Options["X"] != "12" {
		t.Fatalf("numeric string silently coerced: %#v", prepared.Options)
	}
	if err := validateAutomation(prepared); err == nil || !strings.Contains(err.Error(), "string") {
		t.Fatalf("numeric template accepted: %v", err)
	}
}
