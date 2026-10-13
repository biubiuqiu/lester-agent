package sandbox

import (
	"bytes"
	"encoding/json"
	"errors"
)

func (request *FileEditRequest) UnmarshalJSON(data []byte) error {
	type wire FileEditRequest
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range []string{"old_string", "new_string"} {
		value, exists := fields[name]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New(name + " must be explicitly supplied as a string")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded wire
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*request = FileEditRequest(decoded)
	return nil
}
