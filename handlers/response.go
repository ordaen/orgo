package handlers

import (
	"bytes"
	"encoding/json"
)

// MetaData is the meta data of a list response.
type MetaData struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Data   any `json:"data,omitempty"`
}

// Response is the JSON document of a response: {"data": ..., "meta": ..., "errors": [...]}. With errors it has
// only the errors. A nil Data or Meta is left out. Data is encoded by EncodeData with the fields of SetFields.
type Response struct {
	Data any
	Meta *MetaData
	Errors

	fields []string
}

// SetFields sets the fields of the JSON objects of Data, see EncodeData.
func (r *Response) SetFields(fields []string) {
	r.fields = fields
}

// MarshalJSON returns the JSON document.
func (r Response) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	if r.HasErrors() {
		errs, err := json.Marshal(r.Errors.Errors)
		if err != nil {
			return nil, err
		}
		b.WriteString(`"errors":`)
		b.Write(errs)
		b.WriteByte('}')
		return b.Bytes(), nil
	}
	if r.Data != nil {
		data, err := EncodeData(r.Data, r.fields)
		if err != nil {
			return nil, err
		}
		b.WriteString(`"data":`)
		b.Write(data)
	}
	if r.Meta != nil {
		meta, err := json.Marshal(r.Meta)
		if err != nil {
			return nil, err
		}
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		b.WriteString(`"meta":`)
		b.Write(meta)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
