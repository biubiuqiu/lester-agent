package agenttool

import (
	"encoding/json"
	"errors"
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
	return nil
}

func required(value, name string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New(name + " is required")
	}
	return value, nil
}
