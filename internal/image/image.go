// Package image decodes, downscales and re-encodes uploaded pictures.
//
// Everything here runs before an image reaches the database, so the stored
// bytes are always a known type, a bounded size and free of whatever metadata
// the original carried. See docs/adr/0008-images-in-the-database.md.
package image

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	stdimage "image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// The bounding boxes from docs/adr/0008-images-in-the-database.md. Aspect ratio
// is preserved and an image is never upscaled, so these are maxima rather than
// targets.
const (
	MaxWidth  = 800
	MaxHeight = 800

	ThumbMaxWidth  = 200
	ThumbMaxHeight = 200
)

// The three accepted media types, matching the CHECK constraint on the image
// table.
const (
	TypeJPEG = "image/jpeg"
	TypePNG  = "image/png"
	TypeGIF  = "image/gif"
)

// JPEGQuality is what re-encoded JPEGs are written at.
//
// 85 is the usual point where further quality costs bytes without being
// visible. These are menu photographs on an intranet, not print work.
const JPEGQuality = 85

// Errors the pipeline reports. They map to documented API codes: an
// unsupported type is 1012, and a picture that cannot be decoded is 1012 as
// well -- from the caller's side "this is not an image I can read" is one
// problem.
var (
	ErrUnsupportedType = errors.New("image: unsupported media type")
	ErrDecode          = errors.New("image: cannot decode")
	ErrEmpty           = errors.New("image: no data")
)

// Processed is the result of preparing an upload for storage.
type Processed struct {
	MediaType string
	Width     int
	Height    int
	Data      []byte

	ThumbMediaType string
	ThumbWidth     int
	ThumbHeight    int
	ThumbData      []byte

	// SHA256 is of Data, the stored image, not of the upload. Two uploads that
	// differ only in metadata downscale to the same bytes and so deduplicate,
	// which is the behaviour worth having.
	SHA256 []byte
}

// Sniff determines the media type from the content.
//
// The extension and the declared Content-Type are both ignored, because both
// are supplied by the caller and neither is evidence. A .jpg holding a shell
// script is the case this exists for.
//
// http.DetectContentType implements the WHATWG sniffing algorithm and
// recognises far more than three types, so its answer is checked against the
// three that are allowed rather than trusted as-is.
func Sniff(data []byte) (string, error) {
	if len(data) == 0 {
		return "", ErrEmpty
	}

	// DetectContentType looks at the first 512 bytes and never panics on a
	// short slice.
	detected := http.DetectContentType(data)
	if i := strings.IndexByte(detected, ';'); i >= 0 {
		detected = detected[:i]
	}
	detected = strings.TrimSpace(detected)

	switch detected {
	case TypeJPEG, TypePNG, TypeGIF:
		return detected, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedType, detected)
	}
}

// Process decodes, downscales and re-encodes an upload.
//
// The caller has already bounded the byte count; this bounds the pixels. An
// image is decoded once and scaled twice, from the decoded original rather than
// from the already-downscaled main image, so the thumbnail does not compound
// two rounds of resampling.
func Process(data []byte) (Processed, error) {
	mediaType, err := Sniff(data)
	if err != nil {
		return Processed{}, err
	}

	// Decode with the specific decoder rather than image.Decode, so that a
	// file whose content sniffs as one type but decodes as another is refused
	// rather than silently accepted as the second.
	source, err := decode(data, mediaType)
	if err != nil {
		return Processed{}, fmt.Errorf("%w: %w", ErrDecode, err)
	}

	main, mainW, mainH := scaleWithin(source, MaxWidth, MaxHeight)
	thumb, thumbW, thumbH := scaleWithin(source, ThumbMaxWidth, ThumbMaxHeight)

	// The output type is the input type, except that GIF becomes PNG.
	//
	// An animated GIF keeps its first frame only, per the ADR, and a
	// single-frame GIF re-encoded as GIF would be quantised to 256 colours for
	// no reason. PNG is lossless, is one of the three permitted types, and is
	// what a still frame should be.
	outType := mediaType
	if outType == TypeGIF {
		outType = TypePNG
	}

	mainBytes, err := encode(main, outType)
	if err != nil {
		return Processed{}, err
	}
	thumbBytes, err := encode(thumb, outType)
	if err != nil {
		return Processed{}, err
	}

	sum := sha256.Sum256(mainBytes)
	return Processed{
		MediaType:      outType,
		Width:          mainW,
		Height:         mainH,
		Data:           mainBytes,
		ThumbMediaType: outType,
		ThumbWidth:     thumbW,
		ThumbHeight:    thumbH,
		ThumbData:      thumbBytes,
		SHA256:         sum[:],
	}, nil
}

// decode reads an image with the decoder for its sniffed type.
func decode(data []byte, mediaType string) (stdimage.Image, error) {
	r := bytes.NewReader(data)
	switch mediaType {
	case TypeJPEG:
		return jpeg.Decode(r)
	case TypePNG:
		return png.Decode(r)
	case TypeGIF:
		// gif.Decode returns the first frame, which is what the ADR asks for.
		// Using it rather than gif.DecodeAll means an animation's later frames
		// are never allocated at all.
		return gif.Decode(r)
	default:
		return nil, ErrUnsupportedType
	}
}

// encode writes an image in the given type.
func encode(img stdimage.Image, mediaType string) ([]byte, error) {
	var buf bytes.Buffer
	var err error

	switch mediaType {
	case TypeJPEG:
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: JPEGQuality})
	case TypePNG:
		err = png.Encode(&buf, img)
	case TypeGIF:
		err = gif.Encode(&buf, img, nil)
	default:
		return nil, ErrUnsupportedType
	}

	if err != nil {
		return nil, fmt.Errorf("encoding the image: %w", err)
	}
	return buf.Bytes(), nil
}

// scaleWithin fits an image inside a bounding box, preserving aspect ratio.
//
// Never upscales: a 60x60 logo stays 60x60 rather than being blown up to 800
// and losing what sharpness it had. That means the stored image is at most the
// bounding box, not exactly it.
func scaleWithin(source stdimage.Image, maxW, maxH int) (result stdimage.Image, width, height int) {
	bounds := source.Bounds()
	sourceW, sourceH := bounds.Dx(), bounds.Dy()

	targetW, targetH := fit(sourceW, sourceH, maxW, maxH)
	if targetW == sourceW && targetH == sourceH {
		return source, sourceW, sourceH
	}

	// An RGBA rather than the result type, because xdraw.Scale needs a
	// draw.Image to write into and stdimage.Image has no Set.
	canvas := stdimage.NewRGBA(stdimage.Rect(0, 0, targetW, targetH))
	// CatmullRom rather than the cheaper filters: this runs once per upload,
	// and the result is looked at for as long as the restaurant exists.
	xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), source, bounds, xdraw.Over, nil)
	return canvas, targetW, targetH
}

// fit computes the largest size within the box that keeps the aspect ratio.
//
// The result is clamped to at least 1 in each direction: an extremely wide
// image would otherwise scale to a height of zero, which is both a broken
// picture and a CHECK constraint violation.
func fit(width, height, maxW, maxH int) (fittedW, fittedH int) {
	if width <= maxW && height <= maxH {
		return width, height
	}

	// Compare width*maxH against height*maxW rather than dividing, so the
	// decision does not depend on floating point.
	if width*maxH > height*maxW {
		return maxW, atLeastOne(height * maxW / width)
	}
	return atLeastOne(width * maxH / height), maxH
}

func atLeastOne(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
