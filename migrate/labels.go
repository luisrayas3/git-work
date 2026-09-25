package migrate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/schema"
)

// The fields the label taxonomy becomes (store-migration.md).
const (
	statusKey   = "status"
	priorityKey = "priority"
	phaseKey    = "phase"
	areaKey     = "area"
	parentKey   = "parent"
	labelsKey   = "labels"
)

// defaultType is what an issue that never carried a type: label becomes.
const defaultType = "task"

// statusValues maps git-bug's two statuses onto the jira preset's workflow.
var statusValues = map[string]string{
	"open":   "to-do",
	"closed": "done",
}

// typeValues maps a type: label onto a type of the schema.
// A spike has no type of its own: it is a task, and keeps the word as a label.
var typeValues = map[string]string{
	"story":    "story",
	"task":     "task",
	"decision": "decision",
	"spike":    "task",
}

// priorityValues maps a prio: label onto the jira preset's priorities.
var priorityValues = map[string]string{
	"high": "high",
	"med":  "medium",
	"low":  "low",
}

// Needs lists the schema keys the mapping writes into,
// so that `migrate` can refuse before touching anything
// when `git work schema import schema.yaml` has not run.
func Needs() []string {
	var keys []string
	for _, typeKey := range []string{"story", "task", "decision"} {
		for _, field := range []string{statusKey, priorityKey, phaseKey, areaKey, parentKey, labelsKey} {
			keys = append(keys, typeKey+"/"+field)
		}
	}
	return keys
}

// CheckSchema reports the keys the mapping needs and the schema lacks.
func CheckSchema(s *schema.Schema) error {
	var missing []string
	for _, key := range Needs() {
		typeKey, fieldKey, _ := strings.Cut(key, "/")
		if _, ok := s.Field(typeKey, fieldKey); !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("the schema lacks %s; run `git work schema import schema.yaml` first",
			strings.Join(missing, ", "))
	}
	return nil
}

// labelState is what the labels of one issue mean so far:
// the old labels as a set, the way git-bug held them,
// and what each single-valued field holds in the new model,
// so that removing one of two priority labels
// leaves the other rather than clearing the field.
type labelState struct {
	labels map[string]bool
	fields map[string]string
}

func newLabelState() *labelState {
	return &labelState{labels: map[string]bool{}, fields: map[string]string{}}
}

// single-valued fields, by the label prefix that feeds them
type singleField struct {
	prefix string
	key    string
	values map[string]string
	// clearable is false for the type: it is never unset
	clearable bool
}

var singleFields = []singleField{
	{prefix: "type", key: schema.TypeKey, values: typeValues},
	{prefix: "prio", key: priorityKey, values: priorityValues, clearable: true},
	{prefix: "phase", key: phaseKey, clearable: true},
	{prefix: "story", key: parentKey, clearable: true},
}

// multi-valued fields, by the label prefix that feeds them
var multiFields = map[string]string{"area": areaKey}

// firstType is the first type: label an issue was given and the type it names,
// or the default type when it never was.
func firstType(labelOps [][]string) (label string, typeKey string) {
	for _, added := range labelOps {
		for _, label := range added {
			if value, ok := strings.CutPrefix(label, "type:"); ok {
				if typeKey, known := typeValues[value]; known {
					return value, typeKey
				}
			}
		}
	}
	return defaultType, defaultType
}

// convertLabels turns one LabelChange into the field operations it means,
// given what the issue's labels meant before it,
// and records what they mean after.
//
// A single-valued field takes the label added;
// when the label it holds is removed, it takes another of the same prefix
// if one remains, and is cleared otherwise.
// A multi-valued field adds and removes one item per label.
// A type is never cleared.
// A change that means nothing in the new model becomes a no-op operation,
// so that the pack it was in still exists and keeps its times.
func convertLabels(state *labelState, author identity.Interface, unix int64, added, removed []string,
	resolveStory func(prefix string) (entity.Id, error)) ([]issue.Operation, error) {

	for _, label := range added {
		if err := checkLabel(label); err != nil {
			return nil, err
		}
	}
	for _, label := range removed {
		if err := checkLabel(label); err != nil {
			return nil, err
		}
	}

	for _, label := range removed {
		delete(state.labels, label)
	}
	for _, label := range added {
		state.labels[label] = true
	}

	var ops []issue.Operation
	set := func(key string, value issue.Value) {
		ops = append(ops, issue.NewSetFieldOp(author, unix, key, value))
	}

	// single-valued fields
	for _, field := range singleFields {
		var addedValues []string
		for _, label := range added {
			if value, ok := strings.CutPrefix(label, field.prefix+":"); ok {
				addedValues = append(addedValues, value)
			}
		}
		if len(addedValues) > 1 {
			return nil, fmt.Errorf("two %s labels added at once: %s", field.prefix, strings.Join(addedValues, ", "))
		}

		current, has := state.fields[field.key]
		var want string
		wantSome := false
		switch {
		case len(addedValues) == 1:
			want, wantSome = addedValues[0], true
		case has && !state.labels[field.prefix+":"+current]:
			// the label behind the field is gone; another of its prefix?
			var remaining []string
			for label := range state.labels {
				if value, ok := strings.CutPrefix(label, field.prefix+":"); ok {
					remaining = append(remaining, value)
				}
			}
			sort.Strings(remaining)
			if len(remaining) > 0 {
				want, wantSome = remaining[0], true
			}
		default:
			continue
		}

		switch {
		case wantSome && (!has || want != current):
			state.fields[field.key] = want
			value := want
			if field.values != nil {
				value = field.values[want]
			}
			if field.key == parentKey {
				id, err := resolveStory(want)
				if err != nil {
					return nil, err
				}
				value = id.String()
			}
			set(field.key, issue.StringValue(value))
		case !wantSome && has && field.clearable:
			delete(state.fields, field.key)
			set(field.key, issue.MustValue(nil))
		}
	}

	// multi-valued fields, and the labels that stay labels
	item := func(label string, add bool) {
		prefix, value, ok := strings.Cut(label, ":")
		var key string
		switch {
		case !ok:
			key, value = labelsKey, label
		case prefix == "type" && value == "spike":
			// a spike is a task that keeps the word as a label
			key = labelsKey
		case isSingle(prefix):
			return
		default:
			if multi, ok := multiFields[prefix]; ok {
				key = multi
			} else {
				key, value = labelsKey, label
			}
		}
		if add {
			ops = append(ops, issue.NewAddValueOp(author, unix, key, issue.StringValue(value)))
		} else {
			ops = append(ops, issue.NewRemoveValueOp(author, unix, key, issue.StringValue(value)))
		}
	}
	for _, label := range added {
		item(label, true)
	}
	for _, label := range removed {
		item(label, false)
	}

	if len(ops) == 0 {
		ops = append(ops, dag.NewNoOpOp[*issue.Snapshot](issue.NoOpOp, author, unix))
	}
	return ops, nil
}

func isSingle(prefix string) bool {
	for _, field := range singleFields {
		if field.prefix == prefix {
			return true
		}
	}
	return false
}

// checkLabel refuses a taxonomy label whose value has no mapping.
func checkLabel(label string) error {
	prefix, value, ok := strings.Cut(label, ":")
	if !ok {
		return nil
	}
	switch prefix {
	case "type":
		if _, known := typeValues[value]; !known {
			return fmt.Errorf("label %q names no type", label)
		}
	case "prio":
		if _, known := priorityValues[value]; !known {
			return fmt.Errorf("label %q names no priority", label)
		}
	}
	return nil
}
