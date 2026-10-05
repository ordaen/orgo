package types

import (
	"github.com/ordaen/orgo/model"
)

type Record struct {
	ID   model.ID `json:"id"`
	Name string   `json:"name,omitempty"`
	Type string   `json:"type,omitempty"`
}

type DocWithName interface {
	model.Model
	RecordName() string
}

type LinkResponse struct {
	Link string `json:"link"`
}

type InfoType string

const (
	InfoTypeInfo    InfoType = "info"
	InfoTypeSuccess InfoType = "success"
	InfoTypeWarn    InfoType = "warn"
	InfoTypeAlert   InfoType = "alert"
)
