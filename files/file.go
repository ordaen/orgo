package files

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"uuid"

	"github.com/ordaen/orgo/model"
	"github.com/ordaen/orgo/pg"
	"github.com/ordaen/orgo/repo"
	"golang.org/x/image/draw"
)

// avatarSize is the maximum width and height of the avatar images.
const avatarSize = 176

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
		err = jpeg.Encode(buf, thumbnail(img, avatarSize), &jpeg.Options{Quality: 90})
	case "png":
		err = png.Encode(buf, thumbnail(img, avatarSize))
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

// thumbnail scales img down to fit in a size x size square, keeping its aspect ratio.
// An image fitting in the square is returned unchanged.
func thumbnail(img image.Image, size int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= size && h <= size {
		return img
	}
	if w >= h {
		w, h = size, max(1, h*size/w)
	} else {
		w, h = max(1, w*size/h), size
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}
