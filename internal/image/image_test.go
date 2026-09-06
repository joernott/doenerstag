package image_test

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/image"
)

// sample builds an encoded picture of the given size.
//
// A gradient rather than a flat colour, so that a JPEG of it is not degenerate
// and a downscale has something to resample.
func sample(t *testing.T, format string, width, height int) []byte {
	t.Helper()

	img := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{
				R: uint8(x % 256), //nolint:gosec // a coordinate modulo 256 fits
				G: uint8(y % 256), //nolint:gosec // as above
				B: 128,
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "png":
		err = png.Encode(&buf, img)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	default:
		t.Fatalf("unknown format %q", format)
	}
	if err != nil {
		t.Fatalf("encoding a %s sample: %v", format, err)
	}
	return buf.Bytes()
}

func TestSniffRecognisesTheThreeAcceptedTypes(t *testing.T) {
	cases := map[string]string{
		"jpeg": image.TypeJPEG,
		"png":  image.TypePNG,
		"gif":  image.TypeGIF,
	}

	for format, want := range cases {
		got, err := image.Sniff(sample(t, format, 10, 10))
		if err != nil {
			t.Errorf("%s: %v", format, err)
			continue
		}
		if got != want {
			t.Errorf("%s sniffed as %q, want %q", format, got, want)
		}
	}
}

// The extension and the declared type are both supplied by the caller and
// neither is evidence. This is the case the whole sniffing arrangement exists
// for, and the sprint's exit criteria name it.
func TestSniffRejectsNonImages(t *testing.T) {
	cases := map[string][]byte{
		"a shell script":     []byte("#!/bin/sh\nrm -rf /\n"),
		"HTML":               []byte("<!doctype html><title>not an image</title>"),
		"a PDF":              []byte("%PDF-1.7\n1 0 obj\n"),
		"plain text":         []byte("this is just text, at some length to be sniffed"),
		"a zip":              {0x50, 0x4b, 0x03, 0x04, 0, 0, 0, 0},
		"an ELF binary":      {0x7f, 'E', 'L', 'F', 2, 1, 1, 0},
		"a truncated header": {0xff, 0xd8},
	}

	for name, data := range cases {
		if _, err := image.Sniff(data); err == nil {
			t.Errorf("%s was accepted as an image", name)
		}
	}
}

func TestSniffRejectsEmptyInput(t *testing.T) {
	if _, err := image.Sniff(nil); err == nil {
		t.Error("empty input was accepted")
	}
}

// A JPEG header on non-JPEG content must fail at the decode even though it
// sniffs as an image, which is why decoding uses the specific decoder rather
// than image.Decode.
func TestATruncatedImageIsRejected(t *testing.T) {
	full := sample(t, "png", 40, 40)
	truncated := full[:len(full)/2]

	if _, err := image.Process(truncated); err == nil {
		t.Error("a truncated PNG was processed")
	}
}

func TestProcessDownscalesToTheBoundingBox(t *testing.T) {
	processed, err := image.Process(sample(t, "png", 2000, 1000))
	if err != nil {
		t.Fatalf("processing: %v", err)
	}

	if processed.Width != image.MaxWidth {
		t.Errorf("width is %d, want %d", processed.Width, image.MaxWidth)
	}
	// 2000x1000 is 2:1, so fitting 800 wide gives 400 high.
	if processed.Height != 400 {
		t.Errorf("height is %d, want 400", processed.Height)
	}
	if processed.ThumbWidth != image.ThumbMaxWidth || processed.ThumbHeight != 100 {
		t.Errorf("thumbnail is %dx%d, want 200x100",
			processed.ThumbWidth, processed.ThumbHeight)
	}
}

// A tall image is bounded by its height, not its width.
func TestProcessRespectsTheOtherDimension(t *testing.T) {
	processed, err := image.Process(sample(t, "png", 500, 2000))
	if err != nil {
		t.Fatalf("processing: %v", err)
	}

	if processed.Height != image.MaxHeight {
		t.Errorf("height is %d, want %d", processed.Height, image.MaxHeight)
	}
	if processed.Width != 200 {
		t.Errorf("width is %d, want 200", processed.Width)
	}
}

// Never upscale: a small logo stays small rather than being blown up and losing
// what sharpness it had.
func TestProcessNeverUpscales(t *testing.T) {
	processed, err := image.Process(sample(t, "png", 60, 40))
	if err != nil {
		t.Fatalf("processing: %v", err)
	}

	if processed.Width != 60 || processed.Height != 40 {
		t.Errorf("a 60x40 image became %dx%d",
			processed.Width, processed.Height)
	}
	if processed.ThumbWidth != 60 || processed.ThumbHeight != 40 {
		t.Errorf("its thumbnail became %dx%d",
			processed.ThumbWidth, processed.ThumbHeight)
	}
}

// An extreme aspect ratio must not scale to a zero dimension, which would be
// both a broken picture and a CHECK constraint violation.
func TestAnExtremeAspectRatioKeepsBothDimensions(t *testing.T) {
	processed, err := image.Process(sample(t, "png", 4000, 2))
	if err != nil {
		t.Fatalf("processing: %v", err)
	}

	if processed.Height < 1 || processed.ThumbHeight < 1 {
		t.Errorf("a very wide image scaled to height %d (thumb %d)",
			processed.Height, processed.ThumbHeight)
	}
}

// GIF becomes PNG. An animated GIF keeps its first frame only, and re-encoding
// a still frame as GIF would quantise it to 256 colours for no reason.
func TestGIFBecomesPNG(t *testing.T) {
	processed, err := image.Process(sample(t, "gif", 100, 100))
	if err != nil {
		t.Fatalf("processing: %v", err)
	}

	if processed.MediaType != image.TypePNG {
		t.Errorf("a GIF was stored as %q, want %q", processed.MediaType, image.TypePNG)
	}
	if processed.ThumbMediaType != image.TypePNG {
		t.Errorf("its thumbnail is %q", processed.ThumbMediaType)
	}
}

func TestJPEGAndPNGKeepTheirType(t *testing.T) {
	for format, want := range map[string]string{
		"jpeg": image.TypeJPEG,
		"png":  image.TypePNG,
	} {
		processed, err := image.Process(sample(t, format, 100, 100))
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if processed.MediaType != want {
			t.Errorf("%s was stored as %q", format, processed.MediaType)
		}
	}
}

// An animated GIF keeps its first frame only, per the ADR.
func TestAnAnimatedGIFKeepsOneFrame(t *testing.T) {
	frames := &gif.GIF{}
	for i := range 3 {
		frame := stdimage.NewPaletted(stdimage.Rect(0, 0, 50, 50),
			color.Palette{color.Black, color.White})
		frame.SetColorIndex(i, i, 1)
		frames.Image = append(frames.Image, frame)
		frames.Delay = append(frames.Delay, 10)
	}

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, frames); err != nil {
		t.Fatal(err)
	}

	processed, err := image.Process(buf.Bytes())
	if err != nil {
		t.Fatalf("processing an animated GIF: %v", err)
	}

	// The result decodes as a single-image PNG, which is the observable form of
	// "one frame".
	decoded, err := png.Decode(bytes.NewReader(processed.Data))
	if err != nil {
		t.Fatalf("the result is not a PNG: %v", err)
	}
	if decoded.Bounds().Dx() != 50 {
		t.Errorf("the frame is %d wide, want 50", decoded.Bounds().Dx())
	}
}

// The stored bytes are a re-encode, not the upload.
//
// The ADR claims stripping metadata -- EXIF, and with it GPS coordinates -- as
// a privacy benefit of storing a re-encode. This is that claim, tested: bytes
// riding along with the picture do not reach the database.
//
// Trailing data after IEND stands in for a metadata segment. It is what a
// decoder ignores and an encoder cannot reproduce, which is exactly the
// property that makes re-encoding strip things, and it needs no hand-built
// EXIF header to demonstrate.
//
// Note what this does not say: for a lossless format at a size needing no
// downscale, the re-encoded pixels can be byte-identical to the input. That is
// correct, and an earlier version of this test wrongly asserted the bytes must
// differ -- which passed only because the sample had come from a different
// encoder. What matters is that anything that is not pixels is gone.
func TestTheStoredBytesAreARecode(t *testing.T) {
	original := sample(t, "png", 100, 100)
	const secret = "GPS 52.5200N 13.4050E"

	withMetadata := append(append([]byte{}, original...), []byte(secret)...)

	processed, err := image.Process(withMetadata)
	if err != nil {
		t.Fatalf("processing: %v", err)
	}
	if bytes.Contains(processed.Data, []byte(secret)) {
		t.Error("data riding along with the image reached the stored bytes")
	}
	if bytes.Contains(processed.ThumbData, []byte(secret)) {
		t.Error("data riding along with the image reached the thumbnail")
	}
	if len(processed.SHA256) != 32 {
		t.Errorf("the hash is %d bytes, want 32", len(processed.SHA256))
	}

	// And the hash is of the stored bytes, so two uploads differing only in
	// their metadata deduplicate to one row.
	plain, err := image.Process(original)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain.SHA256, processed.SHA256) {
		t.Error("the same picture with and without trailing metadata hashed differently")
	}
}

// The same picture processed twice gives the same bytes and the same hash,
// which is what makes deduplication work at all.
func TestProcessingIsDeterministic(t *testing.T) {
	original := sample(t, "png", 300, 200)

	first, err := image.Process(original)
	if err != nil {
		t.Fatal(err)
	}
	second, err := image.Process(original)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first.SHA256, second.SHA256) {
		t.Error("processing the same image twice gave different hashes")
	}
	if !bytes.Equal(first.Data, second.Data) {
		t.Error("processing the same image twice gave different bytes")
	}
}

// Two different pictures must not collide.
func TestDifferentImagesHashDifferently(t *testing.T) {
	a, err := image.Process(sample(t, "png", 100, 100))
	if err != nil {
		t.Fatal(err)
	}
	b, err := image.Process(sample(t, "png", 120, 100))
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(a.SHA256, b.SHA256) {
		t.Error("two different images hashed the same")
	}
}

// A picture that says it is a JPEG but is a PNG is stored as what it actually
// is, because the type comes from the content.
func TestTheContentDecidesTheType(t *testing.T) {
	pngBytes := sample(t, "png", 50, 50)

	sniffed, err := image.Sniff(pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	if sniffed != image.TypePNG {
		t.Errorf("PNG bytes sniffed as %q", sniffed)
	}

	processed, err := image.Process(pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	if processed.MediaType != image.TypePNG {
		t.Errorf("stored as %q despite being a PNG", processed.MediaType)
	}
}

// The error a caller sees must not leak anything about the server.
func TestErrorsAreDescriptiveNotRevealing(t *testing.T) {
	_, err := image.Sniff([]byte("#!/bin/sh\nid\n"))
	if err == nil {
		t.Fatal("a script was accepted")
	}
	if strings.Contains(err.Error(), "/bin/sh") {
		t.Errorf("the error echoes the uploaded content: %v", err)
	}
}
