package files

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/ordaen/orgo/pg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testImage returns a w x h image encoded as format, "png" or "jpeg".
func testImage(t *testing.T, format string, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 100, A: 255})
		}
	}
	buf := new(bytes.Buffer)
	switch format {
	case "png":
		require.NoError(t, png.Encode(buf, img))
	case "jpeg":
		require.NoError(t, jpeg.Encode(buf, img, nil))
	}
	return buf.Bytes()
}

func TestModelFile(t *testing.T) {
	require.NoError(t, pg.ClearTables("files"))
	data := []byte("test")
	r, err := Files.Create(&File{Name: t.Name(), Type: "attachment", Mime: "text/plain", Data: data})
	require.NoError(t, err)
	assert.Equal(t, "File", pg.ModelType(r))
	assert.Equal(t, t.Name(), r.Name)
	assert.Equal(t, "text/plain", r.Mime)
	assert.Equal(t, data, r.Data)
	assert.NotEmpty(t, r.Token)
}

func TestAvatarResize(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			require.NoError(t, pg.ClearTables("files"))
			r, err := Files.Create(&File{Name: t.Name(), Type: "avatar", Mime: "image/" + format, Data: testImage(t, format, 400, 200)})
			require.NoError(t, err)
			cfg, decoded, err := image.DecodeConfig(bytes.NewReader(r.Data))
			require.NoError(t, err)
			assert.Equal(t, format, decoded)
			assert.Equal(t, 176, cfg.Width)
			assert.Equal(t, 88, cfg.Height)
			assert.Equal(t, len(r.Data), r.Size)
		})
	}
}

func TestThumbnail(t *testing.T) {
	cases := []struct{ w, h, wantW, wantH int }{
		{400, 200, 176, 88},
		{200, 400, 88, 176},
		{300, 300, 176, 176},
		{1000, 2, 176, 1},
		{100, 50, 100, 50},
		{176, 176, 176, 176},
	}
	for _, c := range cases {
		img := image.NewRGBA(image.Rect(10, 10, 10+c.w, 10+c.h))
		b := thumbnail(img, 176).Bounds()
		assert.Equal(t, c.wantW, b.Dx(), "%dx%d", c.w, c.h)
		assert.Equal(t, c.wantH, b.Dy(), "%dx%d", c.w, c.h)
	}
}

func TestAvatarInvalidImage(t *testing.T) {
	_, err := Files.Create(&File{Name: t.Name(), Type: "avatar", Mime: "image/jpeg", Data: []byte("test")})
	assert.ErrorIs(t, err, image.ErrFormat)
}

func TestAttachmentSave(t *testing.T) {
	require.NoError(t, pg.ClearTables("files"))
	data := testImage(t, "png", 10, 10)
	at := &Attachment{Name: "test.png", Mime: "image/png", Enc: "base64", Content: base64.StdEncoding.EncodeToString(data)}
	require.NoError(t, at.Save())
	assert.NotEmpty(t, at.File)
	assert.Empty(t, at.Content)

	file, err := pg.Query(new(File)).Where("token = ?", at.File).First()
	require.NoError(t, err)
	assert.Equal(t, "attachment", file.Type)
	assert.Equal(t, at.Name, file.Name)
	assert.Equal(t, data, file.Data)
	assert.Equal(t, len(data), file.Size)
}
