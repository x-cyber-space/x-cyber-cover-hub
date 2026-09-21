package provider

import "testing"

// TestResizeITunesArtwork covers the trick the whole iTunes path depends on:
// the catalogue encodes the resolution in the artwork URL, so one lookup serves
// any requested size without a second search.
func TestResizeITunesArtwork(t *testing.T) {
	cases := []struct {
		in   string
		size int
		want string
	}{
		{
			"https://is1-ssl.mzstatic.com/image/thumb/Music/a/b/100x100bb.jpg",
			600,
			"https://is1-ssl.mzstatic.com/image/thumb/Music/a/b/600x600bb.jpg",
		},
		{
			"https://is1-ssl.mzstatic.com/image/thumb/Music/a/b/100x100bb.png",
			1200,
			"https://is1-ssl.mzstatic.com/image/thumb/Music/a/b/1200x1200bb.png",
		},
		{
			"https://is1-ssl.mzstatic.com/image/thumb/Music/a/b/60x60bb.jpg",
			300,
			"https://is1-ssl.mzstatic.com/image/thumb/Music/a/b/300x300bb.jpg",
		},
		{
			// An unexpected shape is returned untouched rather than mangled
			// into something that would 404.
			"https://example.com/artwork.jpg",
			600,
			"https://example.com/artwork.jpg",
		},
	}

	for _, c := range cases {
		if got := resizeITunesArtwork(c.in, c.size); got != c.want {
			t.Errorf("resizeITunesArtwork(%q, %d)\n got %q\nwant %q", c.in, c.size, got, c.want)
		}
	}
}

func TestJoinNonEmpty(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"周杰伦", "叶惠美"}, "周杰伦 叶惠美"},
		{[]string{"", "叶惠美"}, "叶惠美"},
		{[]string{"  ", "  "}, ""},
		{[]string{"未知歌曲", "周杰伦"}, "未知歌曲 周杰伦"},
	}
	for _, c := range cases {
		if got := joinNonEmpty(c.in...); got != c.want {
			t.Errorf("joinNonEmpty(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
