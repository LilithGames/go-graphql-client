package graphql

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
)

type constructOptionsOutput struct {
	operationName       string
	operationDirectives []string
}

func (coo constructOptionsOutput) OperationDirectivesString() string {
	operationDirectivesStr := strings.Join(coo.operationDirectives, " ")
	if operationDirectivesStr != "" {
		return fmt.Sprintf(" %s ", operationDirectivesStr)
	}
	return ""
}

func constructOptions(options []Option) (*constructOptionsOutput, error) {
	output := &constructOptionsOutput{}

	for _, option := range options {
		switch option.Type() {
		case optionTypeOperationName:
			output.operationName = option.String()
		case OptionTypeOperationDirective:
			output.operationDirectives = append(output.operationDirectives, option.String())
		default:
			return nil, fmt.Errorf("invalid query option type: %s", option.Type())
		}
	}

	return output, nil
}

func constructQuery(v interface{}, variables map[string]interface{}, options ...Option) (string, error) {
	query := query(v)

	optionsOutput, err := constructOptions(options)
	if err != nil {
		return "", err
	}

	if len(variables) > 0 {
		return fmt.Sprintf("query %s(%s)%s%s", optionsOutput.operationName, queryArguments(variables), optionsOutput.OperationDirectivesString(), query), nil
	}

	if optionsOutput.operationName == "" && len(optionsOutput.operationDirectives) == 0 {
		return query, nil
	}

	return fmt.Sprintf("query %s%s%s", optionsOutput.operationName, optionsOutput.OperationDirectivesString(), query), nil
}

func constructMutation(v interface{}, variables map[string]interface{}, options ...Option) (string, error) {
	query := query(v)
	optionsOutput, err := constructOptions(options)
	if err != nil {
		return "", err
	}
	if len(variables) > 0 {
		return fmt.Sprintf("mutation %s(%s)%s%s", optionsOutput.operationName, queryArguments(variables), optionsOutput.OperationDirectivesString(), query), nil
	}

	if optionsOutput.operationName == "" && len(optionsOutput.operationDirectives) == 0 {
		return "mutation" + query, nil
	}

	return fmt.Sprintf("mutation %s%s%s", optionsOutput.operationName, optionsOutput.OperationDirectivesString(), query), nil
}

func constructSubscription(v interface{}, variables map[string]interface{}, options ...Option) (string, error) {
	query := query(v)
	optionsOutput, err := constructOptions(options)
	if err != nil {
		return "", err
	}
	if len(variables) > 0 {
		return fmt.Sprintf("subscription %s(%s)%s%s", optionsOutput.operationName, queryArguments(variables), optionsOutput.OperationDirectivesString(), query), nil
	}
	if optionsOutput.operationName == "" && len(optionsOutput.operationDirectives) == 0 {
		return "subscription" + query, nil
	}
	return fmt.Sprintf("subscription %s%s%s", optionsOutput.operationName, optionsOutput.OperationDirectivesString(), query), nil
}

// queryArguments constructs a minified arguments string for variables.
//
// E.g., map[string]interface{}{"a": Int(123), "b": NewBoolean(true)} -> "$a:Int!$b:Boolean".
func queryArguments(variables map[string]interface{}) string {
	// Sort keys in order to produce deterministic output for testing purposes.
	// TODO: If tests can be made to work with non-deterministic output, then no need to sort.
	keys := make([]string, 0, len(variables))
	for k := range variables {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, k := range keys {
		io.WriteString(&buf, "$")
		io.WriteString(&buf, k)
		io.WriteString(&buf, ":")
		writeArgumentType(&buf, reflect.TypeOf(variables[k]), true)
		// Don't insert a comma here.
		// Commas in GraphQL are insignificant, and we want minified output.
		// See https://facebook.github.io/graphql/October2016/#sec-Insignificant-Commas.
	}
	return buf.String()
}

// writeArgumentType writes a minified GraphQL type for t to w.
// value indicates whether t is a value (required) type or pointer (optional) type.
// If value is true, then "!" is written at the end of t.
func writeArgumentType(w io.Writer, t reflect.Type, value bool) {
	if t.Kind() == reflect.Ptr {
		// Pointer is an optional type, so no "!" at the end of the pointer's underlying type.
		writeArgumentType(w, t.Elem(), false)
		return
	}

	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		// List. E.g., "[Int]".
		io.WriteString(w, "[")
		writeArgumentType(w, t.Elem(), true)
		io.WriteString(w, "]")
	default:
		// Named type. E.g., "Int".
		name := t.Name()
		if name == "string" { // HACK: Workaround for https://github.com/shurcooL/githubv4/issues/12.
			name = "ID"
		}
		io.WriteString(w, name)
	}

	if value {
		// Value is a required type, so add "!" to the end.
		io.WriteString(w, "!")
	}
}

// query uses writeQuery to recursively construct
// a minified query string from the provided struct v.
//
// E.g., struct{Foo Int, BarBaz *Boolean} -> "{foo,barBaz}".
func query(v interface{}) string {
	var buf bytes.Buffer
	writeQuery(&buf, reflect.TypeOf(v), false)
	return buf.String()
}

// writeQuery writes a minified query for t to w.
// If inline is true, the struct fields of t are inlined into parent struct.
func writeQuery(w io.Writer, t reflect.Type, inline bool) {
	switch t.Kind() {
	case reflect.Ptr, reflect.Slice:
		writeQuery(w, t.Elem(), false)
	case reflect.Struct:
		// If the type implements json.Unmarshaler, it's a scalar. Don't expand it.
		if reflect.PtrTo(t).Implements(jsonUnmarshaler) {
			return
		}
		if !inline {
			io.WriteString(w, "{")
		}
		for i := 0; i < t.NumField(); i++ {
			if i != 0 {
				io.WriteString(w, ",")
			}
			f := t.Field(i)
			value, ok := f.Tag.Lookup("graphql")
			inlineField := f.Anonymous && !ok
			if !inlineField {
				if ok {
					io.WriteString(w, value)
				} else {
					io.WriteString(w, f.Name)
				}
			}
			writeQuery(w, f.Type, inlineField)
		}
		if !inline {
			io.WriteString(w, "}")
		}
	}
}

var jsonUnmarshaler = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
