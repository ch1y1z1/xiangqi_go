package recognition

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// PrepareImage decodes the first image/frame, applies EXIF orientation (all eight
// transforms), downsizes without upscaling, flattens alpha on white, and writes
// quality-88 JPEG without source metadata. Supported: JPEG, PNG, GIF, BMP, TIFF
// and static WebP. HEIC/HEIF, AVIF and animated WebP are not supported.
func PrepareImage(data []byte) ([]byte, error) {
	const maxInputBytes = 64 << 20
	const maxPixels = 48_000_000
	if len(data) > maxInputBytes {
		return nil, errors.New("图片文件过大，请选择不超过 64 MiB 的图片。")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, imageFormatError()
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxPixels {
		return nil, errors.New("图片尺寸过大，请选择不超过 4800 万像素的图片。")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, imageFormatError()
	}
	orientation := imageOrientation(data)
	view := orientedImage{Image: src, orientation: orientation}
	bounds := view.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w > 2048 || h > 2048 {
		if w >= h {
			h = max(1, h*2048/w)
			w = 2048
		} else {
			w = max(1, w*2048/h)
			h = 2048
		}
	}
	output := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(output, output.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(output, output.Bounds(), view, bounds, draw.Over, nil)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, output, &jpeg.Options{Quality: 88}); err != nil {
		return nil, errors.New("图片转换失败，请重新选择。")
	}
	return encoded.Bytes(), nil
}

func imageFormatError() error {
	return errors.New("无法打开图片，文件已损坏或格式不支持。支持 JPEG、PNG、GIF、BMP、TIFF 和静态 WebP；HEIC/HEIF、AVIF 请先转换为 JPEG 或 PNG。")
}

// A coordinate view avoids allocating a second full-resolution image just to
// rotate it. Scaling only allocates the final image (at most 2048 x 2048).
type orientedImage struct {
	image.Image
	orientation int
}

func (v orientedImage) Bounds() image.Rectangle {
	b := v.Image.Bounds()
	if v.orientation >= 5 {
		return image.Rect(0, 0, b.Dy(), b.Dx())
	}
	return image.Rect(0, 0, b.Dx(), b.Dy())
}
func (v orientedImage) At(x, y int) color.Color {
	b := v.Image.Bounds()
	w, h := b.Dx(), b.Dy()
	switch v.orientation {
	case 2:
		x = w - 1 - x
	case 3:
		x, y = w-1-x, h-1-y
	case 4:
		y = h - 1 - y
	case 5:
		x, y = y, x
	case 6:
		x, y = y, h-1-x
	case 7:
		x, y = w-1-y, h-1-x
	case 8:
		x, y = w-1-y, x
	}
	return v.Image.At(b.Min.X+x, b.Min.Y+y)
}
