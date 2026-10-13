package agenttool

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func decodeArguments(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("tool arguments must contain one JSON object")
	}
	return nil
}

// A required string may intentionally be empty, but omission and null are not
// equivalent to that choice (especially for overwrite and deletion).
func requiredStringField(raw json.RawMessage, name string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	value, exists := fields[name]
	if !exists || strings.TrimSpace(string(value)) == "null" {
		return fmt.Errorf("%s is required and must be a string; use an explicit empty string if intended", name)
	}
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil {
		return fmt.Errorf("%s must be a string", name)
	}
	return nil
}

func required(value, name string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New(name + " is required")
	}
	return value, nil
}
