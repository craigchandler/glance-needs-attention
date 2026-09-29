package adapters

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	cfgpkg "github.com/craigchandler/glance-needs-attention/internal/config"
)

type GenericJSONAdapter struct{}

type genericSettings struct {
	Rules []genericRule `json:"rules"`
}

type genericRule struct {
	ForEach      string `json:"forEach,omitempty"`
	Path         string `json:"path"`
	Op           string `json:"op"`
	Value        any    `json:"value,omitempty"`
	Severity     string `json:"severity,omitempty"`
	SeverityPath string `json:"severityPath,omitempty"`
	Message      string `json:"message"`
}

func (GenericJSONAdapter) Check(ctx context.Context, client *http.Client, source cfgpkg.SourceConfig) Result {
	var settings genericSettings
	if err := decodeSettings(source.Settings, &settings); err != nil {
		return Result{Errors: []string{source.Name + ": invalid settings: " + err.Error()}}
	}
	var doc any
	if err := getJSON(ctx, client, source.URL, &doc); err != nil {
		return Result{Errors: []string{source.Name + ": " + err.Error()}}
	}
	var out Result
	for _, rule := range settings.Rules {
		if strings.TrimSpace(rule.ForEach) == "" {
			evaluateGenericRule(&out, source.Name, doc, rule)
			continue
		}

		itemsValue, ok := lookupPath(doc, rule.ForEach)
		if !ok || itemsValue == nil {
			// A missing or null collection means there are currently no items to
			// evaluate. JSON encoders commonly emit null for an uninitialized
			// empty slice.
			continue
		}
		items, ok := itemsValue.([]any)
		if !ok {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: JSON path %q is not an array", source.Name, rule.ForEach))
			continue
		}
		for _, item := range items {
			evaluateGenericRule(&out, source.Name, item, rule)
		}
	}
	return out
}

func evaluateGenericRule(out *Result, sourceName string, doc any, rule genericRule) {
	actual, ok := lookupPath(doc, rule.Path)
	if !ok {
		out.Errors = append(out.Errors, fmt.Sprintf("%s: JSON path %q not found", sourceName, rule.Path))
		return
	}
	matched, err := compare(actual, rule.Op, rule.Value)
	if err != nil {
		out.Errors = append(out.Errors, fmt.Sprintf("%s: rule %q: %v", sourceName, rule.Path, err))
		return
	}
	if !matched {
		return
	}

	sev := severity(rule.Severity, "warning")
	if rule.SeverityPath != "" {
		if v, ok := lookupPath(doc, rule.SeverityPath); ok {
			sev = severity(fmt.Sprint(v), sev)
		}
	}
	msg := rule.Message
	if msg == "" {
		msg = fmt.Sprintf("%s %s %v", rule.Path, rule.Op, rule.Value)
	}
	msg = renderMessage(msg, doc, actual)
	out.Alerts = append(out.Alerts, Alert{Source: sourceName, Severity: sev, Message: msg})
}

func renderMessage(message string, doc any, actual any) string {
	message = strings.ReplaceAll(message, "{{value}}", fmt.Sprint(actual))
	m, ok := doc.(map[string]any)
	if !ok {
		return message
	}
	for key, value := range m {
		message = strings.ReplaceAll(message, "{{"+key+"}}", fmt.Sprint(value))
	}
	return message
}

func lookupPath(doc any, path string) (any, bool) {
	cur := doc
	for _, part := range strings.Split(strings.TrimSpace(path), ".") {
		if part == "" {
			continue
		}
		switch x := cur.(type) {
		case map[string]any:
			v, ok := x[part]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			cur = x[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func compare(actual any, op string, expected any) (bool, error) {
	op = strings.ToLower(strings.TrimSpace(op))
	switch op {
	case "eq", "ne":
		equal := reflect.DeepEqual(actual, expected) || fmt.Sprint(actual) == fmt.Sprint(expected)
		if op == "ne" {
			return !equal, nil
		}
		return equal, nil
	case "gt", "gte", "lt", "lte":
		a, ok := toFloat(actual)
		if !ok {
			return false, fmt.Errorf("actual value is not numeric")
		}
		e, ok := toFloat(expected)
		if !ok {
			return false, fmt.Errorf("expected value is not numeric")
		}
		switch op {
		case "gt":
			return a > e, nil
		case "gte":
			return a >= e, nil
		case "lt":
			return a < e, nil
		default:
			return a <= e, nil
		}
	case "contains":
		return strings.Contains(fmt.Sprint(actual), fmt.Sprint(expected)), nil
	case "truthy":
		switch v := actual.(type) {
		case bool:
			return v, nil
		case string:
			return v != "" && v != "0" && !strings.EqualFold(v, "false"), nil
		case float64:
			return v != 0, nil
		case nil:
			return false, nil
		default:
			return true, nil
		}
	case "falsy":
		b, err := compare(actual, "truthy", nil)
		return !b, err
	default:
		return false, fmt.Errorf("unsupported operator %q", op)
	}
}
