package gql

import (
	"encoding/json"
	"errors"
	"io"
)

// JSON is a GraphQL scalar of any JSON value
type JSON json.RawMessage

// MarshalGQL implements the graphql.Marshaler interface, an empty JSON is null
func (a JSON) MarshalGQL(w io.Writer) {
	if len(a) == 0 {
		io.WriteString(w, "null")
		return
	}
	w.Write(a)
}

// UnmarshalGQL implements the graphql.Unmarshaler interface. A string or bytes must be a JSON document,
// the other values, like the objects and lists of the GraphQL input, are encoded as JSON.
func (a *JSON) UnmarshalGQL(v any) error {
	var b []byte
	switch t := v.(type) {
	case string:
		b = []byte(t)
	case []byte:
		b = t
	default:
		enc, err := json.Marshal(t)
		if err != nil {
			return err
		}
		*a = enc
		return nil
	}
	if !json.Valid(b) {
		return errors.New("invalid JSON")
	}
	*a = JSON(b)
	return nil
}
