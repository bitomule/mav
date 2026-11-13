package mav

import (
	"fmt"
	"reflect"
)

// transformAutomationStrings applies the flow's existing expansion mechanism
// to option values. Each invocation owns its containers, so retries can bind
// the original operation again without retaining an earlier attempt's values.
// Keys and non-string scalars keep their types and values. Typed validation is
// separate: a numeric option supplied as a template string remains a string
// and must be rejected by validateAutomation, rather than coerced implicitly.
func transformAutomationStrings(req Automation, transform func(string) (string, error)) (Automation, error) {
	if req.Options == nil {
		return req, nil
	}
	value, err := transformAutomationValue(reflect.ValueOf(req.Options), transform)
	if err != nil {
		return Automation{}, fmt.Errorf("automation options: %w", err)
	}
	req.Options = value.Interface().(map[string]any)
	return req, nil
}

func transformAutomationValue(value reflect.Value, transform func(string) (string, error)) (reflect.Value, error) {
	if !value.IsValid() {
		return value, nil
	}
	switch value.Kind() {
	case reflect.String:
		expanded, err := transform(value.String())
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(expanded).Convert(value.Type()), nil
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		child, err := transformAutomationValue(value.Elem(), transform)
		if err != nil {
			return reflect.Value{}, err
		}
		copy := reflect.New(value.Type()).Elem()
		if value.Kind() == reflect.Pointer {
			copy.Set(reflect.New(value.Type().Elem()))
			copy.Elem().Set(child)
		} else {
			copy.Set(child)
		}
		return copy, nil
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		copy := reflect.MakeMapWithSize(value.Type(), value.Len())
		entries := value.MapRange()
		for entries.Next() {
			child, err := transformAutomationValue(entries.Value(), transform)
			if err != nil {
				return reflect.Value{}, err
			}
			copy.SetMapIndex(entries.Key(), child)
		}
		return copy, nil
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		copy := reflect.New(value.Type()).Elem()
		if value.Kind() == reflect.Slice {
			copy = reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		}
		for i := 0; i < value.Len(); i++ {
			child, err := transformAutomationValue(value.Index(i), transform)
			if err != nil {
				return reflect.Value{}, err
			}
			copy.Index(i).Set(child)
		}
		return copy, nil
	case reflect.Struct:
		copy := reflect.New(value.Type()).Elem()
		copy.Set(value)
		for i := 0; i < value.NumField(); i++ {
			if !copy.Field(i).CanSet() || value.Type().Field(i).PkgPath != "" {
				continue
			}
			child, err := transformAutomationValue(value.Field(i), transform)
			if err != nil {
				return reflect.Value{}, err
			}
			copy.Field(i).Set(child)
		}
		return copy, nil
	default:
		return value, nil
	}
}
