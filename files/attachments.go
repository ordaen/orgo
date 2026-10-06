package files

import (
	"encoding/base64"
)

// Attachment is an uploaded file, with its Content encoded by Enc, "base64" or none. Save stores it as a File
// and replaces its content with the token of the file.
type Attachment struct {
	File    string `json:"file"`
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	Enc     string `json:"enc"`
	Content string `json:"content,omitempty"`
}

// Save stores the decoded content as an attachment File, then sets File to its token and clears Content.
func (m *Attachment) Save() error {
	file := &File{
		Name: m.Name,
		Mime: m.Mime,
		Type: "attachment",
	}

	switch m.Enc {
	case "base64":
		b, err := base64.StdEncoding.DecodeString(m.Content)
		if err != nil {
			return err
		}
		file.Data = b
	default:
		file.Data = []byte(m.Content)
	}
	file.Size = len(file.Data)

	if _, err := Files.Create(file); err != nil {
		return err
	}
	m.File = file.Token
	m.Content = ""
	return nil
}
