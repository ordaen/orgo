package settings

import (
	"encoding/json"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
)

// FindByKey returns the stored setting of the key, or a new setting when it is not stored or the query fails.
func FindByKey(key string) *Setting {
	rec, err := findByKey(key)
	if err != nil {
		return new(Setting)
	}
	return rec
}

// findByKey returns the stored setting of the key, or pg.ErrRecordNotFound.
func findByKey(key string) (*Setting, error) {
	return pg.Query(&Setting{}).Where("type = ?", key).First()
}

func newRecord(kind string, v any) (*Setting, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &Setting{Type: kind, Data: b}, nil
}

// Setting model, the JSON data of the settings registered with the key Type
type Setting struct {
	model.Base[model.ID]

	Type string
	Data json.RawMessage
}

func (m *Setting) TableName() string { return "settings" }
