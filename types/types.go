// Package types defines the small types shared by the orgo packages and the applications: the users acting on
// records, the hashed passwords, the variables and the record references.
package types

import (
	"github.com/ordaen/orgo/model"
)

// Record is a reference to a record, by its ID, with its name and type.
type Record struct {
	ID   model.ID `json:"id"`
	Name string   `json:"name,omitempty"`
	Type string   `json:"type,omitempty"`
}

// DocWithName is a model with a name.
type DocWithName interface {
	model.Model
	// RecordName returns the name of the record.
	RecordName() string
}

// LinkResponse is a response with a link, like a download link.
type LinkResponse struct {
	Link string `json:"link"`
}

// InfoType is the kind of a message shown to the user.
type InfoType string

// The kinds of the messages shown to the user.
const (
	InfoTypeInfo    InfoType = "info"
	InfoTypeSuccess InfoType = "success"
	InfoTypeWarn    InfoType = "warn"
	InfoTypeAlert   InfoType = "alert"
)
