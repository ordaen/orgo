package files

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"uuid"

	"github.com/nfnt/resize"
	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
)

var _ pg.BeforeCreateHook = (*File)(nil)

var Files = repo.Register(&filesStore{})

type filesStore struct {
	repo.Base[*File]
}

// File model
type File struct {
	model.Base[model.ID]

	Token string `json:"token,readonly"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Mime  string `json:"mime"`
	Size  int    `json:"size"`
	Data  []byte `json:"data,omitempty"`
}

func (m *File) TableName() string {
	return "files"
}

func (m *File) BeforeCreate(ctx context.Context, tx pg.Tx) error {
	m.Token = uuid.NewV4().String()
	if m.Type != "avatar" {
		return nil
	}

	img, mime, err := image.Decode(bytes.NewReader(m.Data))
	if err != nil {
		return err
	}
	buf := new(bytes.Buffer)
	switch mime {
	case "jpeg":
		resized := resize.Thumbnail(176, 176, img, resize.NearestNeighbor)
		err = jpeg.Encode(buf, resized, &jpeg.Options{Quality: 90})
	case "png":
		resized := resize.Thumbnail(176, 176, img, resize.NearestNeighbor)
		err = png.Encode(buf, resized)
	default:
		return fmt.Errorf("unsupported image format '%s'", mime)
	}
	if err != nil {
		return err
	}

	m.Size = buf.Len()
	m.Data = buf.Bytes()
	return nil
}
