package matching

import (
	"testing"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

func TestNormalizeIdentity(t *testing.T) {
	cases := []struct{ in, want string }{
		{"叶惠美", "叶惠美"},
		{"叶惠美 (Deluxe Edition)", "叶惠美"},
		{"叶惠美（豪华版）", "叶惠美"},
		{"1989 (Taylor's Version)", "1989"},
		{"1989 [Remastered]", "1989"},
		{"  Abbey   Road  ", "abbey road"},
	}
	for _, c := range cases {
		if got := NormalizeIdentity(c.in); got != c.want {
			t.Errorf("NormalizeIdentity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMatchCoverStages pins the (artist, album) identity. The album is what a
// cover belongs to, so an artist agreement alone is never an exact match.
func TestMatchCoverStages(t *testing.T) {
	cases := []struct {
		name          string
		q             model.Query
		artist, album string
		want          MatchStage
		why           string
	}{
		{
			name:   "both fields equal",
			q:      model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美"},
			artist: "周杰伦", album: "叶惠美",
			want: MatchExact,
		},
		{
			name:   "edition suffix is stripped from the album",
			q:      model.Query{ArtistName: "Taylor Swift", AlbumName: "1989"},
			artist: "Taylor Swift", album: "1989 (Deluxe Edition)",
			want: MatchExact,
			why:  "stripping brackets is what keeps an edition from being a separate fetch",
		},
		{
			name:   "multi-artist credit overlaps",
			q:      model.Query{ArtistName: "周杰伦", AlbumName: "依然范特西"},
			artist: "周杰伦 / 费玉清", album: "依然范特西",
			want: MatchRelaxed,
		},
		{
			name:   "different album by the same artist",
			q:      model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美"},
			artist: "周杰伦", album: "七里香",
			want: MatchNone,
			why:  "the whole point: a different record means different artwork",
		},
		{
			name:   "different artist, same album title",
			q:      model.Query{ArtistName: "光良", AlbumName: "童话"},
			artist: "王菲", album: "童话",
			want: MatchNone,
		},
		{
			name:   "artist only cannot confirm an album",
			q:      model.Query{ArtistName: "周杰伦"},
			artist: "周杰伦", album: "叶惠美",
			want: MatchRelaxed,
			why:  "no album in the request means no album identity to confirm",
		},
		{
			name:   "album containment",
			q:      model.Query{ArtistName: "五月天", AlbumName: "自传"},
			artist: "五月天", album: "自传 人生有限公司",
			want: MatchRelaxed,
		},
	}

	for _, c := range cases {
		if got := MatchCover(c.q, c.artist, c.album); got != c.want {
			t.Errorf("%s: MatchCover = %v, want %v (%s)", c.name, got, c.want, c.why)
		}
	}
}

// TestScoreCoverWeightsAlbumAboveArtist documents the ranking intent: for
// artwork the album is the identity, so it outweighs the artist. Compare the
// sibling lyrics service, which weights the title highest.
func TestScoreCoverWeightsAlbumAboveArtist(t *testing.T) {
	q := model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美"}

	sameAlbumWrongArtist := ScoreCover(q, "某位无关歌手", "叶惠美")
	sameArtistWrongAlbum := ScoreCover(q, "周杰伦", "七里香")

	if sameAlbumWrongArtist <= sameArtistWrongAlbum {
		t.Errorf("a matching album must outweigh a matching artist: album=%v artist=%v",
			sameAlbumWrongArtist, sameArtistWrongAlbum)
	}
}

func TestScoreCoverExactIsMaximal(t *testing.T) {
	q := model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美"}
	if got := ScoreCover(q, "周杰伦", "叶惠美 (Deluxe)"); got != 100 {
		t.Errorf("an exact album and artist should score 100, got %v", got)
	}
}

func TestClampSize(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, DefaultSize},
		{-5, DefaultSize},
		{1, MinSize},
		{600, 600},
		{99999, MaxSize},
	}
	for _, c := range cases {
		if got := ClampSize(c.in); got != c.want {
			t.Errorf("ClampSize(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestIdentityKeyIsPerAlbumNotPerTrack is the defect this service exists to fix:
// the consuming player caches artwork per song id, so a twelve-track album
// fetches and stores the same image twelve times. Keying on (artist, album,
// size) collapses that to one.
func TestIdentityKeyIsPerAlbumNotPerTrack(t *testing.T) {
	a := IdentityKey("周杰伦", "叶惠美", 600)
	b := IdentityKey("周杰伦", "叶惠美", 600)
	if a != b {
		t.Fatal("the same album must produce the same key")
	}
	if a == IdentityKey("周杰伦", "七里香", 600) {
		t.Error("different albums must not collide")
	}
	if a == IdentityKey("周杰伦", "叶惠美", 1200) {
		t.Error("different sizes must not collide")
	}
}

func TestArtistsOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"周杰伦", "周杰伦 / 费玉清", true},
		{"周杰伦 / 费玉清", "周杰伦", true},
		{"Beyond feat. 黄家驹", "Beyond", true},
		{"周杰伦", "王菲", false},
		{"AC/DC", "AC/DC", true},
		{"", "周杰伦", false},
	}
	for _, c := range cases {
		if got := ArtistsOverlap(c.a, c.b); got != c.want {
			t.Errorf("ArtistsOverlap(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
