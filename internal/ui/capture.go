package ui

import (
	"errors"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// captureResult is what came of one rule of a request: the variable it
// names, and why it could not be set from the response if it could not.
type captureResult struct {
	variable string
	err      error
}

// The icons which say how capturing went, once they have been drawn.
var capturedIcon, notCapturedIcon *qt.QIcon

// captureIcon returns a tick for a value that was captured, and a cross for
// one that was not.
func captureIcon(captured bool) *qt.QIcon {
	if captured {
		if capturedIcon == nil {
			capturedIcon = drawIcon(iconSize*2, themeColour(colourCaptured), drawTick)
		}
		return capturedIcon
	}
	if notCapturedIcon == nil {
		notCapturedIcon = drawIcon(iconSize*2, themeColour(colourNotCaptured), drawCross)
	}
	return notCapturedIcon
}

// newCaptureTable returns the table of the Capture tab of a response panel:
// the variables to set from the response, each with the path of its value in the
// JSON of the response, and a regular expression to pick out part of that
// value with. The expression is in the column of descriptions.
func newCaptureTable() *keyValueTable {
	table := newKeyValueTable("Environment Variable", "Path in the response, e.g. data.items[0].id")
	// The expression is no less a part of the rule than the path is.
	table.plain = true
	table.describe("Regex (optional)")
	table.setItems(nil)
	// Each row is marked with how capturing it went.
	table.table.SetColumnHidden(colStatus, false)
	return table
}

// captureRules returns the rules in a table made by newCaptureTable.
func captureRules(table *keyValueTable) []capture.Rule {
	var rules []capture.Rule
	for _, pair := range table.allDescribed() {
		rules = append(rules, capture.Rule{Variable: pair.Key, Path: pair.Value, Regex: pair.Description, Enabled: pair.Enabled})
	}
	return rules
}

// setCaptureRules shows rules in a table made by newCaptureTable.
func setCaptureRules(table *keyValueTable, rules []capture.Rule) {
	pairs := make([]describedPair, len(rules))
	for i, rule := range rules {
		pairs[i] = describedPair{rest.KeyValue{Key: rule.Variable, Value: rule.Path, Enabled: rule.Enabled}, rule.Regex}
	}
	table.setDescribed(pairs)
}

// capture sets variables of the active environment from the JSON of a
// response, as the rules of the request say. It returns what came of each
// rule that is in use.
func (w *Window) capture(rules []capture.Rule, document string) []captureResult {
	rules = enabledCaptures(rules)
	if len(rules) == 0 {
		return nil
	}

	environment, ok := w.environments.store.Current()
	if !ok {
		return failedCaptures(rules, errors.New("no environment is selected to keep it in"))
	}

	results := make([]captureResult, len(rules))
	changed := false
	for i, rule := range rules {
		value, err := rule.Extract(document)
		results[i] = captureResult{rule.Variable, err}
		if err == nil {
			environment.Set(rule.Variable, value)
			changed = true
		}
	}

	if changed {
		// Saving also refreshes what depends on the environment.
		w.environments.save()
		if w.stack.CurrentIndex() == pageEditor {
			w.editor.load(environment)
		}
	}
	return results
}

// failedCaptures returns the results of rules none of which could be
// followed, all for the same reason.
func failedCaptures(rules []capture.Rule, err error) []captureResult {
	var results []captureResult
	for _, rule := range enabledCaptures(rules) {
		results = append(results, captureResult{rule.Variable, err})
	}
	return results
}

// enabledCaptures returns the rules that are in use: those that are enabled
// and name a variable.
func enabledCaptures(rules []capture.Rule) []capture.Rule {
	var enabled []capture.Rule
	for _, rule := range rules {
		if rule.Variable = strings.TrimSpace(rule.Variable); rule.Enabled && rule.Variable != "" {
			enabled = append(enabled, rule)
		}
	}
	return enabled
}
