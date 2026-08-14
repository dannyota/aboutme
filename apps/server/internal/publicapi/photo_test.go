package publicapi

import "testing"

func TestPublicPhotoAcceptsNormalizedImageTypes(t *testing.T) {
	for _, contentType := range []string{"image/jpeg", "image/png"} {
		response, err := NewPublicPhoto([]byte("image"), contentType)
		if err != nil || response.Header.Get("Content-Type") != contentType {
			t.Fatalf("NewPublicPhoto(%q) = %#v, %v", contentType, response, err)
		}
	}
	if _, err := NewPublicPhoto([]byte("image"), "image/webp"); err == nil {
		t.Fatal("accepted unsupported image type")
	}
}
