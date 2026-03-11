package pagination

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
)

func Encode(value interface{}) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func Decode(cursor string, out interface{}) error {
	if cursor == "" {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func ClampLimit(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	val, err := strconv.Atoi(raw)
	if err != nil || val <= 0 {
		return def
	}
	if val > max {
		return max
	}
	return val
}
