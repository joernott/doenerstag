package api_test

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

type imageResponse struct {
	ID               string `json:"id"`
	MediaType        string `json:"media_type"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	ByteSize         int    `json:"byte_size"`
	ThumbMediaType   string `json:"thumb_media_type"`
	ThumbWidth       int    `json:"thumb_width"`
	ThumbHeight      int    `json:"thumb_height"`
	SHA256           string `json:"sha256"`
	OriginalFilename string `json:"original_filename"`
}

// samplePNG builds an encoded picture of the given size.
func samplePNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{
				R: uint8(x % 256), //nolint:gosec // a coordinate modulo 256 fits
				G: uint8(y % 256), //nolint:gosec // as above
				B: 64, A: 255,
			})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding a sample: %v", err)
	}
	return buf.Bytes()
}

func sampleJPEG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: 200, B: uint8(y % 256), A: 255}) //nolint:gosec // coordinates modulo 256 fit
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encoding a sample: %v", err)
	}
	return buf.Bytes()
}

// uploadFile posts a multipart body, which the JSON helpers cannot build.
func (f *apiFixture) uploadFile(
	filename string, content []byte, cookies []*http.Cookie,
) *httptest.ResponseRecorder {
	f.t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(api.ImageFormField, filename)
	if err != nil {
		f.t.Fatalf("building the multipart body: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		f.t.Fatalf("writing the file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		f.t.Fatalf("closing the multipart body: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, api.APIPrefix+"/images", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.RemoteAddr = "192.0.2.55:41234"
	for _, c := range cookies {
		r.AddCookie(c)
		if c.Name == api.CSRFCookieName {
			r.Header.Set(api.CSRFHeaderName, c.Value)
		}
	}

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	return rec
}

// uploadImage uploads a sample and returns the created record.
func (f *apiFixture) uploadImage(cookies []*http.Cookie) imageResponse {
	f.t.Helper()

	rec := f.uploadFile("logo.png", samplePNG(f.t, 300, 200), cookies)
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("uploading: %d %s", rec.Code, rec.Body.String())
	}
	var body imageResponse
	decode(f.t, rec, &body)
	return body
}

func TestUploadingAnImage(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("anna")

	rec := f.uploadFile("Logo.PNG", samplePNG(t, 1600, 800), cookies)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var body imageResponse
	decode(t, rec, &body)
	if body.MediaType != "image/png" {
		t.Errorf("media type is %q", body.MediaType)
	}
	if body.Width != 800 || body.Height != 400 {
		t.Errorf("stored at %dx%d, want 800x400", body.Width, body.Height)
	}
	if body.ThumbWidth != 200 || body.ThumbHeight != 100 {
		t.Errorf("thumbnail is %dx%d, want 200x100", body.ThumbWidth, body.ThumbHeight)
	}
	if len(body.SHA256) != 64 {
		t.Errorf("the hash is %q, want 64 hex characters", body.SHA256)
	}
	if body.OriginalFilename != "Logo.PNG" {
		t.Errorf("the filename is %q", body.OriginalFilename)
	}
	if body.ByteSize <= 0 {
		t.Error("the stored size is not reported")
	}
}

// The sprint's exit criterion: uploading a non-image with a .jpg name is
// rejected. The extension and the declared type are both the caller's word,
// and neither is evidence.
func TestAFileWhoseNameLiesIsRejected(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("bertil")

	cases := map[string][]byte{
		"a shell script called .jpg": []byte("#!/bin/sh\nrm -rf /\n"),
		"HTML called .jpg":           []byte("<!doctype html><script>alert(1)</script>"),
		"a PDF called .jpg":          []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n"),
		"a zip called .png":          {0x50, 0x4b, 0x03, 0x04, 0, 0, 0, 0, 0, 0, 0, 0},
		"plain text called .gif":     []byte("nothing here is an image at all, honestly"),
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			rec := f.uploadFile("innocent.jpg", content, cookies)
			expectError(t, rec, http.StatusUnsupportedMediaType, api.CodeImageUnsupportedType)

			// The error must not echo what was uploaded.
			if strings.Contains(rec.Body.String(), "rm -rf") ||
				strings.Contains(rec.Body.String(), "<script>") {
				t.Errorf("the error echoes the uploaded content: %s", rec.Body.String())
			}
		})
	}
}

// A real image with a wrong extension is accepted and stored as what it is,
// because the type comes from the content in both directions.
func TestARealImageWithAWrongExtensionIsAccepted(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("carla")

	rec := f.uploadFile("actually-a-png.jpg", samplePNG(t, 100, 100), cookies)
	if rec.Code != http.StatusCreated {
		t.Fatalf("a genuine PNG named .jpg was rejected: %s", rec.Body.String())
	}

	var body imageResponse
	decode(t, rec, &body)
	if body.MediaType != "image/png" {
		t.Errorf("stored as %q, want image/png -- the content decides", body.MediaType)
	}
}

func TestATruncatedImageIsRejected(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("dora")

	full := samplePNG(t, 200, 200)
	rec := f.uploadFile("truncated.png", full[:len(full)/3], cookies)
	expectError(t, rec, http.StatusUnsupportedMediaType, api.CodeImageUnsupportedType)
}

func TestAnOversizedUploadIsRejected(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("erik")

	// A picture large enough to exceed the fixture's cap once encoded. The
	// gradient keeps PNG from compressing it away to nothing.
	big := samplePNG(t, 1200, 1200)
	if len(big) <= fixtureMaxImageBytes {
		t.Skipf("the sample is only %d bytes, under the %d cap",
			len(big), fixtureMaxImageBytes)
	}

	rec := f.uploadFile("huge.png", big, cookies)
	expectError(t, rec, http.StatusRequestEntityTooLarge, api.CodeImageTooLarge)
}

func TestUploadingNeedsALogin(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.uploadFile("logo.png", samplePNG(t, 50, 50), nil)
	expectError(t, rec, http.StatusUnauthorized, api.CodeNotAuthenticated)
}

// The same picture uploaded twice is stored once, because two people adding the
// same logo is a normal thing to do.
func TestTheSamePictureIsStoredOnce(t *testing.T) {
	f := newAPIFixture(t)
	first := f.register("frieda")
	second := f.register("gustav")

	content := samplePNG(t, 400, 300)

	a := f.uploadFile("mine.png", content, first)
	b := f.uploadFile("theirs.png", content, second)
	if a.Code != http.StatusCreated || b.Code != http.StatusCreated {
		t.Fatalf("an upload failed: %d, %d", a.Code, b.Code)
	}

	var one, two imageResponse
	decode(t, a, &one)
	decode(t, b, &two)

	if one.ID != two.ID {
		t.Errorf("the same picture was stored twice: %s and %s", one.ID, two.ID)
	}
	if one.SHA256 != two.SHA256 {
		t.Error("the same picture hashed differently")
	}

	var rows int
	if err := f.pool.QueryRow(f.ctx(), `SELECT count(*) FROM image`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("the image table has %d rows, want 1", rows)
	}
}

func TestDifferentPicturesAreStoredSeparately(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("hanna")

	a := f.uploadFile("one.png", samplePNG(t, 100, 100), cookies)
	b := f.uploadFile("two.png", samplePNG(t, 120, 100), cookies)

	var one, two imageResponse
	decode(t, a, &one)
	decode(t, b, &two)
	if one.ID == two.ID {
		t.Error("two different pictures were stored as one")
	}
}

func TestServingAnImageAndItsThumbnail(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("ida")
	uploaded := f.uploadImage(cookies)

	for _, path := range []string{
		"/images/" + uploaded.ID,
		"/images/" + uploaded.ID + "/thumbnail",
	} {
		// Anonymously: images are public, because a restaurant list needs its
		// logos before anybody logs in.
		rec := f.get(path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s answered %d: %s", path, rec.Code, rec.Body.String())
			continue
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
			t.Errorf("%s served %q", path, ct)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s served no bytes", path)
		}
		if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
			t.Errorf("%s did not serve a valid PNG: %v", path, err)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s does not send nosniff", path)
		}
	}
}

// The thumbnail is smaller than the image, which is the only reason it exists.
func TestTheThumbnailIsSmaller(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("jonas")
	uploaded := f.uploadImage(cookies)

	full := f.get("/images/" + uploaded.ID)
	thumb := f.get("/images/" + uploaded.ID + "/thumbnail")

	if thumb.Body.Len() >= full.Body.Len() {
		t.Errorf("the thumbnail is %d bytes and the image %d",
			thumb.Body.Len(), full.Body.Len())
	}

	decoded, err := png.Decode(bytes.NewReader(thumb.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() > 200 || decoded.Bounds().Dy() > 200 {
		t.Errorf("the thumbnail is %v, larger than 200x200", decoded.Bounds())
	}
}

// An image's bytes never change, so they may be cached hard and revalidated
// with an ETag.
func TestImagesAreCacheableAndRevalidate(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("klara")
	uploaded := f.uploadImage(cookies)
	path := "/images/" + uploaded.ID

	first := f.get(path)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag was sent")
	}
	if !strings.Contains(etag, uploaded.SHA256) {
		t.Errorf("the ETag %q is not derived from the content hash", etag)
	}
	if cc := first.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control is %q", cc)
	}

	// A matching ETag gets 304 and no body.
	revalidated := f.do(request{
		method: http.MethodGet, path: path,
		headers: map[string]string{"If-None-Match": etag},
	})
	if revalidated.Code != http.StatusNotModified {
		t.Errorf("revalidation answered %d, want 304", revalidated.Code)
	}
	if revalidated.Body.Len() != 0 {
		t.Errorf("a 304 carried %d bytes", revalidated.Body.Len())
	}

	// A non-matching one gets the bytes.
	stale := f.do(request{
		method: http.MethodGet, path: path,
		headers: map[string]string{"If-None-Match": `"something else"`},
	})
	if stale.Code != http.StatusOK {
		t.Errorf("a stale ETag answered %d, want 200", stale.Code)
	}
}

// The image and its thumbnail are different resources, so their ETags must not
// collide -- a shared tag would serve one where the other was asked for.
func TestTheThumbnailHasItsOwnETag(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("lena")
	uploaded := f.uploadImage(cookies)

	full := f.get("/images/" + uploaded.ID).Header().Get("ETag")
	thumb := f.get("/images/" + uploaded.ID + "/thumbnail").Header().Get("ETag")

	if full == thumb {
		t.Errorf("the image and its thumbnail share the ETag %s", full)
	}

	// And the image's tag must not produce a 304 on the thumbnail.
	rec := f.do(request{
		method: http.MethodGet, path: "/images/" + uploaded.ID + "/thumbnail",
		headers: map[string]string{"If-None-Match": full},
	})
	if rec.Code == http.StatusNotModified {
		t.Error("the image's ETag revalidated the thumbnail")
	}
}

func TestUnknownImageIsNotFound(t *testing.T) {
	f := newAPIFixture(t)

	for _, path := range []string{
		"/images/018f0000-0000-7000-8000-00000000dead",
		"/images/not-a-uuid",
		"/images/018f0000-0000-7000-8000-00000000dead/thumbnail",
	} {
		if rec := f.get(path); rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, rec.Code)
		}
	}
}

// A JPEG stays a JPEG, so the served type follows the stored one.
func TestAJPEGIsServedAsJPEG(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("malte")

	rec := f.uploadFile("photo.jpg", sampleJPEG(t, 300, 300), cookies)
	if rec.Code != http.StatusCreated {
		t.Fatalf("uploading: %s", rec.Body.String())
	}
	var body imageResponse
	decode(t, rec, &body)
	if body.MediaType != "image/jpeg" {
		t.Fatalf("stored as %q", body.MediaType)
	}

	served := f.get("/images/" + body.ID)
	if ct := served.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("served as %q", ct)
	}
	if _, err := jpeg.Decode(bytes.NewReader(served.Body.Bytes())); err != nil {
		t.Errorf("the served bytes are not a JPEG: %v", err)
	}
}

// A filename is stored and echoed back, so it gets the same treatment as any
// other string a user supplies.
func TestTheFilenameIsSanitised(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("nils")

	// No control-character cases. Go's multipart layer does not let one through
	// unchanged: a NUL is refused outright, and CR/LF are percent-escaped into
	// the header, so the handler receives the literal text "%0D%0A" and there
	// is nothing to strip. sanitiseFilename still strips them, as defence in
	// depth for a filename arriving some other way, but that path cannot be
	// driven through HTTP and this test does not pretend to cover it.
	//
	// Each case uses a differently-sized picture. With identical ones the
	// uploads deduplicate to a single row, every response comes back carrying
	// the first-stored filename, and the assertions quietly stop testing
	// anything -- which is exactly what happened here until an unrelated change
	// altered Go's map iteration order and exposed it.
	cases := []struct {
		given string
		want  string
	}{
		{"../../etc/passwd", "passwd"},
		{`C:\Users\me\logo.png`, "logo.png"},
		{"/tmp/logo.png", "logo.png"},
		{"  spaced.png  ", "spaced.png"},
		{strings.Repeat("a", 400), strings.Repeat("a", 255)},
	}

	for i, tc := range cases {
		rec := f.uploadFile(tc.given, samplePNG(t, 40+i*7, 40+i*3), cookies)
		if rec.Code != http.StatusCreated {
			t.Errorf("%q: %s", tc.given, rec.Body.String())
			continue
		}
		var body imageResponse
		decode(t, rec, &body)
		if body.OriginalFilename != tc.want {
			t.Errorf("%q was stored as %q, want %q", tc.given, body.OriginalFilename, tc.want)
		}
	}
}

// 6.8: a restaurant carries a logo by id.
func TestARestaurantCanCarryALogo(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("olga")
	logo := f.uploadImage(cookies)

	rec := f.post("/restaurants", map[string]any{
		"name": "Mit Logo", "currency_code": "EUR",
		"logo_image_id": logo.ID,
		"contacts": []map[string]any{
			{"contact_type_id": f.contactTypeID("phone"), "value": "+49 30 1"},
		},
	}, cookies...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating with a logo: %s", rec.Body.String())
	}

	var created restaurantResponse
	decode(t, rec, &created)
	if created.LogoImageID == nil || *created.LogoImageID != logo.ID {
		t.Fatalf("the logo is %v, want %s", created.LogoImageID, logo.ID)
	}

	// The list carries it too, so a list page can show logos without a request
	// per restaurant.
	list := f.get("/restaurants")
	var body struct {
		Restaurants []restaurantResponse `json:"restaurants"`
	}
	decode(t, list, &body)
	var found bool
	for _, restaurant := range body.Restaurants {
		if restaurant.ID == created.ID {
			found = true
			if restaurant.LogoImageID == nil || *restaurant.LogoImageID != logo.ID {
				t.Error("the list does not carry the logo id")
			}
		}
	}
	if !found {
		t.Error("the restaurant is missing from the list")
	}
}

func TestALogoCanBeAttachedAndRemovedLater(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("paula")
	restaurant := f.createRestaurant("Später", cookies)
	logo := f.uploadImage(cookies)

	attached := f.patch("/restaurants/"+restaurant.ID, map[string]any{
		"logo_image_id": logo.ID,
	}, cookies...)
	if attached.Code != http.StatusOK {
		t.Fatalf("attaching: %s", attached.Body.String())
	}
	var withLogo restaurantResponse
	decode(t, attached, &withLogo)
	if withLogo.LogoImageID == nil {
		t.Fatal("the logo was not attached")
	}

	removed := f.patch("/restaurants/"+restaurant.ID, map[string]any{
		"logo_image_id": nil,
	}, cookies...)
	var withoutLogo restaurantResponse
	decode(t, removed, &withoutLogo)
	if withoutLogo.LogoImageID != nil {
		t.Errorf("the logo was not removed: %v", withoutLogo.LogoImageID)
	}
}

func TestAnUnknownLogoIsRejected(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("quirin")
	restaurant := f.createRestaurant("Kein Logo", cookies)

	rec := f.patch("/restaurants/"+restaurant.ID, map[string]any{
		"logo_image_id": "018f0000-0000-7000-8000-00000000dead",
	}, cookies...)
	if rec.Code < 400 {
		t.Errorf("an unknown logo id was accepted: %s", rec.Body.String())
	}

	malformed := f.patch("/restaurants/"+restaurant.ID, map[string]any{
		"logo_image_id": "not-a-uuid",
	}, cookies...)
	expectError(t, malformed, http.StatusBadRequest, api.CodeInvalidField)
}
