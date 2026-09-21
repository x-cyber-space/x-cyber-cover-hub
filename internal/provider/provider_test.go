package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pngHeader is a 1x1 PNG. Its first bytes are what DetectContentType matches on.
var pngHeader = []byte{
	0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R',
}

// TestDownloadImageTrustsBytesOverHeader is a regression test: the NetEase
// image host labels PNG bodies as "image/jpg", so a client that believed the
// header would write a .jpg file full of PNG data.
func TestDownloadImageTrustsBytesOverHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpg") // a lie
		_, _ = w.Write(pngHeader)
	}))
	defer srv.Close()

	_, contentType, err := downloadImage(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("downloadImage failed: %v", err)
	}
	if contentType != "image/png" {
		t.Errorf("content type = %q, want image/png (sniffed from the bytes)", contentType)
	}
}

// TestDownloadImageRejectsNonImages keeps a misbehaving upstream, or one
// serving an error page with a 200, from being cached as artwork.
func TestDownloadImageRejectsNonImages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not an image</html>"))
	}))
	defer srv.Close()

	if _, _, err := downloadImage(context.Background(), srv.URL, ""); err == nil {
		t.Fatal("expected an error for a non-image body")
	}
}

func TestDownloadImageRejectsEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
	}))
	defer srv.Close()

	if _, _, err := downloadImage(context.Background(), srv.URL, ""); err == nil {
		t.Fatal("expected an error for an empty body")
	}
}

func TestDownloadImageRejectsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	_, _, err := downloadImage(context.Background(), srv.URL, "")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected the upstream status in the error, got %v", err)
	}
}
