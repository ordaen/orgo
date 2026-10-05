package model

import (
	"errors"
	"fmt"
	"io"
	"strconv"
)

// ErrInvalidIDFormat is returned by ID.UnmarshalJSON when the value is not an integer.
var ErrInvalidIDFormat = errors.New("invalid ID format")

// ModelID is the ID of a Model, like ID or UUID.
type ModelID interface {
	String() string
	Valid() bool
}

// ID is an integer model ID, like a Bigserial column. It is encoded as a string in JSON and GraphQL.
type ID int

// String returns the ID in decimal.
func (id ID) String() string {
	return strconv.Itoa(int(id))
}

// Valid reports whether the ID is greater than 0, 0 is the ID of a record that is not inserted yet.
func (id ID) Valid() bool {
	return id > 0
}

// UnmarshalGQL implements the graphql.Unmarshaler interface. The value must be a string, an empty string is 0.
func (id *ID) UnmarshalGQL(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("ID must be a string")
	}

	if s == "" {
		*id = 0
		return nil
	}

	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*id = ID(n)
	return nil
}

// MarshalGQL implements the graphql.Marshaler interface. The ID is written as a string, 0 as an empty string.
func (id ID) MarshalGQL(w io.Writer) {
	if id == 0 {
		w.Write([]byte(`""`))
	} else {
		w.Write([]byte(strconv.Quote(strconv.Itoa(int(id)))))
	}
}

// MarshalJSON implements the json.Marshaler interface. The ID is written as a string, like "1".
func (id ID) MarshalJSON() ([]byte, error) {
	return []byte("\"" + id.String() + "\""), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface. It accepts a number or a string with a number,
// an empty value leaves the ID unchanged. It returns ErrInvalidIDFormat when the value is not an integer.
func (id *ID) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if b[0] == '"' {
		b = b[1:]
	}
	if b[len(b)-1] == '"' {
		b = b[:len(b)-1]
	}
	if len(b) == 0 {
		return nil
	}
	n, err := strconv.Atoi(string(b))
	if err != nil {
		return ErrInvalidIDFormat
	}
	*id = ID(n)
	return nil
}

// UUID is a string model ID, like a UUID column.
type UUID string

// String returns the UUID as it is.
func (uuid UUID) String() string {
	return string(uuid)
}

// Valid reports whether the UUID is not empty.
func (uuid UUID) Valid() bool {
	return uuid != ""
}

// MarshalJSON implements the json.Marshaler interface. The UUID is written as a string.
func (uuid UUID) MarshalJSON() ([]byte, error) {
	return []byte("\"" + uuid.String() + "\""), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface. An empty value leaves the UUID unchanged.
func (uuid *UUID) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if b[0] == '"' {
		b = b[1:]
	}
	if b[len(b)-1] == '"' {
		b = b[:len(b)-1]
	}
	if len(b) == 0 {
		return nil
	}
	*uuid = UUID(string(b))
	return nil
}

// MarshalGQL implements the graphql.Marshaler interface. The UUID is written as a string.
func (uuid UUID) MarshalGQL(w io.Writer) {
	if uuid == "" {
		w.Write([]byte(`""`))
	} else {
		w.Write([]byte(strconv.Quote(uuid.String())))
	}
}

// UnmarshalGQL implements the graphql.Unmarshaler interface. The value must be a string.
func (uuid *UUID) UnmarshalGQL(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("UUID must be a string")
	}
	if s == "" {
		*uuid = ""
		return nil
	}
	*uuid = UUID(s)
	return nil
}
