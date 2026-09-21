package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

// NetEaseProvider reads album artwork from the NetEase Cloud Music public
// cloudsearch endpoint.
//
// It is the fallback source for the Chinese catalogue, which the iTunes store
// often lacks entirely — searching iTunes for 周杰伦 returns unrelated Western
// records, so this source is what actually serves most Chinese albums.
type NetEaseProvider struct{}

// NewNetEaseProvider returns the NetEase artwork source.
func NewNetEaseProvider() *NetEaseProvider { return &NetEaseProvider{} }

func (p *NetEaseProvider) Name() string { return "netease" }

const netEaseReferer = "https://music.163.com"

// cloudsearch type codes.
const (
	netEaseTypeSong  = 1
	netEaseTypeAlbum = 10
)

// The album search shape: result.albums[] carries the artwork directly.
type netEaseAlbumSearchResp struct {
	Result struct {
		Albums []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			PicURL string `json:"picUrl"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"albums"`
	} `json:"result"`
}

// The song search shape, used only when the request has no album name: the
// album has to be lifted out of a track result.
type netEaseSongSearchResp struct {
	Result struct {
		Songs []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Ar   []struct {
				Name string `json:"name"`
			} `json:"ar"`
			Al struct {
				ID     int64  `json:"id"`
				Name   string `json:"name"`
				PicURL string `json:"picUrl"`
			} `json:"al"`
		} `json:"songs"`
	} `json:"result"`
}

// Search looks for albums when the request names one, and for tracks otherwise.
//
// Searching albums rather than songs is the difference between finding 叶惠美
// and finding a track whose title merely contains the word 周杰伦: a song
// search for "周杰伦 叶惠美" returns neither the album nor anything close to it.
func (p *NetEaseProvider) Search(ctx context.Context, q model.Query, limit int) ([]*model.Candidate, error) {
	if limit <= 0 {
		limit = 5
	}

	if strings.TrimSpace(q.AlbumName) != "" {
		return p.searchAlbums(ctx, q, limit)
	}
	return p.searchSongs(ctx, q, limit)
}

func (p *NetEaseProvider) searchAlbums(ctx context.Context, q model.Query, limit int) ([]*model.Candidate, error) {
	term := joinNonEmpty(q.AlbumName, q.ArtistName)
	if term == "" {
		return nil, nil
	}

	rawURL := fmt.Sprintf("https://music.163.com/api/cloudsearch/pc?s=%s&type=%d&limit=%d&offset=0",
		url.QueryEscape(term), netEaseTypeAlbum, limit)

	var resp netEaseAlbumSearchResp
	if err := getJSON(ctx, rawURL, netEaseReferer, &resp); err != nil {
		return nil, err
	}

	candidates := make([]*model.Candidate, 0, len(resp.Result.Albums))
	for _, a := range resp.Result.Albums {
		if strings.TrimSpace(a.PicURL) == "" {
			continue
		}
		candidates = append(candidates, &model.Candidate{
			Source:     p.Name(),
			AlbumID:    fmt.Sprint(a.ID),
			ArtistName: a.Artist.Name,
			AlbumName:  a.Name,
			ArtworkURL: upgradeToHTTPS(a.PicURL),
		})
	}
	return candidates, nil
}

func (p *NetEaseProvider) searchSongs(ctx context.Context, q model.Query, limit int) ([]*model.Candidate, error) {
	term := joinNonEmpty(q.TrackName, q.ArtistName)
	if term == "" {
		return nil, nil
	}

	rawURL := fmt.Sprintf("https://music.163.com/api/cloudsearch/pc?s=%s&type=%d&limit=%d&offset=0",
		url.QueryEscape(term), netEaseTypeSong, limit)

	var resp netEaseSongSearchResp
	if err := getJSON(ctx, rawURL, netEaseReferer, &resp); err != nil {
		return nil, err
	}

	candidates := make([]*model.Candidate, 0, len(resp.Result.Songs))
	for _, s := range resp.Result.Songs {
		if strings.TrimSpace(s.Al.PicURL) == "" {
			continue
		}
		names := make([]string, 0, len(s.Ar))
		for _, a := range s.Ar {
			names = append(names, a.Name)
		}
		candidates = append(candidates, &model.Candidate{
			Source:     p.Name(),
			AlbumID:    fmt.Sprint(s.Al.ID),
			TrackName:  s.Name,
			ArtistName: strings.Join(names, " / "),
			AlbumName:  s.Al.Name,
			ArtworkURL: upgradeToHTTPS(s.Al.PicURL),
		})
	}
	return candidates, nil
}

// FetchCover asks NetEase for the artwork at the requested size.
//
// Unlike iTunes, the size is a query parameter the image host honours
// (?param=600y600), so the URL has to be rebuilt rather than rewritten.
func (p *NetEaseProvider) FetchCover(ctx context.Context, c *model.Candidate, size int) (*model.Cover, error) {
	if strings.TrimSpace(c.ArtworkURL) == "" {
		return nil, ErrNoArtwork
	}
	artURL := fmt.Sprintf("%s?param=%dy%d", c.ArtworkURL, size, size)

	data, contentType, err := downloadImage(ctx, artURL, netEaseReferer)
	if err != nil {
		return nil, err
	}
	return &model.Cover{
		ArtistName:  c.ArtistName,
		AlbumName:   c.AlbumName,
		Source:      p.Name(),
		ArtworkURL:  artURL,
		ContentType: contentType,
		Size:        size,
		Data:        data,
	}, nil
}

// upgradeToHTTPS rewrites the scheme the search endpoint returns.
//
// cloudsearch hands back http:// image URLs, and the album detail endpoint
// serves the same object over https — so the upgrade costs nothing and avoids
// mixed-content blocking in browser clients.
func upgradeToHTTPS(rawURL string) string {
	if strings.HasPrefix(rawURL, "http://") {
		return "https://" + strings.TrimPrefix(rawURL, "http://")
	}
	return rawURL
}
