package main

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

// flexString decodes either a JSON string or a JSON number. OKX is inconsistent
// across endpoints and API versions: some responses encode chainIndex as "1" and
// others as 1, and numeric amounts may also switch representation. Number
// literals are kept exactly as sent so large integer amounts never lose precision.
type flexString string

func (s *flexString) UnmarshalJSON(data []byte) error {
	raw := bytes.TrimSpace(data)
	value := jsontext.Value(raw)
	if !value.IsValid() {
		return fmt.Errorf("invalid JSON value %q", raw)
	}

	switch value.Kind() {
	case jsontext.KindNull:
		*s = ""
		return nil
	case jsontext.KindString:
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
		*s = flexString(text)
		return nil
	case jsontext.KindNumber:
		*s = flexString(raw)
		return nil
	default:
		return fmt.Errorf("expected a string or number, got %s", raw)
	}
}

func (s flexString) String() string {
	return string(s)
}

// dexProtocolList accepts either a single protocol object or an array of them.
// OKX returns an object when a route uses one protocol and an array when it
// splits the route across several.
type dexProtocolList []dexProtocol

func (l *dexProtocolList) UnmarshalJSON(data []byte) error {
	raw := bytes.TrimSpace(data)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		*l = nil
		return nil
	}

	switch raw[0] {
	case '[':
		var items []dexProtocol
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		*l = items
		return nil
	case '{':
		var item dexProtocol
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		*l = []dexProtocol{item}
		return nil
	default:
		return fmt.Errorf("expected a protocol object or array, got %s", raw)
	}
}
