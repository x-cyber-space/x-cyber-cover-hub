package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

// NetEaseProvider reads album artwork from the NetEase Cloud Music public
// search endpoint. It is the fallback source: it covers the Chinese catalogue
// that the iTunes store often lacks, but its image URLs are referer-checked and
// fixed-size unless asked otherwise.
type NetEaseProvider struct{}

// NewNetEaseProvider returns the NetEase artwork source.
func NewNetEaseProvider() *NetEaseProvider { return &NetEaseProvider{} }

func (p *NetEaseProvider) Name() string { return "netease" }

const netEaseReferer = "https://music.163.com"

type netEaseSearchResp struct {
	Result struct {
		Songs []struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Artists []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Album struct {
				ID     int64  `json:"id"`
				Name   string `json:"name"`
				PicURL string `json:"picUrl"`
			} `json:"album"`
		} `json:"songs"`
	} `json:"result"`
}

// Search uses the song endpoint and lifts the album out of each result, which
// is how the artwork is reachable; NetEase has no equivalent of looking an
// album up by name alone with artwork attached.
func (p *NetEaseProvider) Search(ctx context.Context, q model.Query, limit int) ([]*model.Candidate, error) {
	if limit <= 0 {
		limit = 5
	}

	term := joinNonEmpty(q.TrackName, q.AlbumName, q.ArtistName)
	if term == "" {
		term = joinNonEmpty(q.AlbumName, q.ArtistName)
	}
	if term == "" {
		return nil, nil
	}

	rawURL := fmt.Sprintf(
		"https://music.163.com/api/search/get/web?csrf_token=&type=1&offset=0&total=true&limit=%d&s=%s",
		limit, url.QueryEscape(term))

	var resp netEaseSearchResp
	if err := getJSON(ctx, rawURL, netEaseReferer, &resp); err != nil {
		return nil, err
	}

	candidates := make([]*model.Candidate, 0, len(resp.Result.Songs))
	for _, s := range resp.Result.Songs {
		if strings.TrimSpace(s.Album.PicURL) == "" {
			continue
		}
		names := make([]string, 0, len(s.Artists))
		for _, a := range s.Artists {
			names = append(names, a.Name)
		}
		candidates = append(candidates, &model.Candidate{
			Source:     p.Name(),
			AlbumID:    fmt.Sprint(s.Album.ID),
			TrackName:  s.Name,
			ArtistName: strings.Join(names, " / "),
			AlbumName:  s.Album.Name,
			ArtworkURL: s.Album.PicURL,
		})
	}
	return candidates, nil
}

// FetchCover asks NetEase for the artwork at the requested size.
//
// Unlike iTunes, the size is a query parameter that the image host honours
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
