package mav

import (
	"fmt"
	"sort"
	"strings"
)

// SuccessCondition preserves goto's conjunctive route/text syntax and adds
// exact id/value predicates for action goals. Model declarations cannot alter it.
type SuccessCondition struct {
	ArrivalCriterion
	Values map[string]string
}

func ParseSuccessCondition(spec string) (SuccessCondition, error) {
	out := SuccessCondition{Values: map[string]string{}}
	var route []string
	for _, term := range splitTerms(spec) {
		if strings.HasPrefix(term, "value:") {
			v := unquote(strings.TrimPrefix(term, "value:"))
			id, value, ok := strings.Cut(v, "=")
			if !ok || strings.TrimSpace(id) == "" {
				return out, fmt.Errorf("value: requires an exact accessibility id=value pair")
			}
			if _, exists := out.Values[id]; exists {
				return out, fmt.Errorf("duplicate value predicate for %q", id)
			}
			out.Values[id] = value
		} else {
			route = append(route, term)
		}
	}
	a, err := ParseArrivalCriterion(strings.Join(route, " "))
	if err != nil {
		return out, fmt.Errorf("%s", strings.ReplaceAll(err.Error(), "--arrived-when", "--succeeded-when"))
	}
	out.ArrivalCriterion = a
	return out, nil
}

func (s SuccessCondition) Declared() bool { return !s.ArrivalCriterion.IsZero() || len(s.Values) > 0 }

// Expected field values remain local until the caller's selected input is
// delivered. The model gets predicate names, never a declaration's secret value.
func (s SuccessCondition) PlanningHint() string {
	redacted := SuccessCondition{ArrivalCriterion: s.ArrivalCriterion, Values: map[string]string{}}
	for id := range s.Values {
		redacted.Values[id] = "<caller-declared expected value>"
	}
	return redacted.String()
}

func (s SuccessCondition) String() string {
	parts := []string{s.ArrivalCriterion.String()}
	keys := make([]string, 0, len(s.Values))
	for k := range s.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, `value:"`+k+`=`+s.Values[k]+`"`)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}
func (s SuccessCondition) Satisfied(o GoalObservation) bool {
	if !s.Declared() {
		return false
	}
	visible := make([]Element, 0, len(o.Elements))
	for _, el := range o.Elements {
		if el.Visible != "false" {
			visible = append(visible, el)
		}
	}
	if !s.ArrivalCriterion.IsZero() && !s.MatchesRoute(ExtractRoute(visible), visible) {
		return false
	}
	for id, value := range s.Values {
		count := 0
		for _, el := range o.Elements {
			if el.ID == id && isOfferableState(el) {
				count++
				if el.Value != value {
					return false
				}
			}
		}
		if count != 1 {
			return false
		}
	}
	return true
}
