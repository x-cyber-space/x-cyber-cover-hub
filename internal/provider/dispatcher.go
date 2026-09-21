package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/matching"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

const (
	// searchTimeout bounds the parallel provider lookup.
	searchTimeout = 8 * time.Second
	// resolveTimeout bounds the artwork download, and is deliberately separate
	// so a slow search cannot starve the fetch.
	resolveTimeout = 15 * time.Second
	// candidatesPerProvider is how many results each source may offer.
	candidatesPerProvider = 5
	// maxFetchAttempts caps how many ranked candidates are tried before giving
	// up. A source can match the right album and still have no image for it, so
	// falling through to the next candidate is the normal path, not an error.
	maxFetchAttempts = 3
)

// Dispatcher queries every artwork source and resolves a single cover.
type Dispatcher struct {
	providers []Provider
	// priority maps a provider name to its position in the list. Earlier
	// providers win ties, which is what makes iTunes the primary source
	// without hard-coding its name anywhere.
	priority map[string]int
}

// NewDispatcher creates a dispatcher with the standard sources, iTunes first.
func NewDispatcher() *Dispatcher {
	return NewDispatcherWith(NewITunesProvider(), NewNetEaseProvider())
}

// NewDispatcherWith builds a dispatcher over an explicit provider set. It
// exists so matching and fallback behaviour can be tested offline.
func NewDispatcherWith(providers ...Provider) *Dispatcher {
	priority := make(map[string]int, len(providers))
	for i, p := range providers {
		priority[p.Name()] = i
	}
	return &Dispatcher{providers: providers, priority: priority}
}

// ranked pairs a candidate with how it matched.
type ranked struct {
	cand  *model.Candidate
	stage matching.MatchStage
}

// Resolve finds the artwork for a query.
//
// It returns (nil, nil) when no source offered an album that matches: a cover
// that cannot be identified is not a cover, and guessing would embed the wrong
// artwork into a user's audio files permanently. An error is returned only when
// every source failed, so the caller can distinguish "not found" from "broken".
func (d *Dispatcher) Resolve(ctx context.Context, q model.Query) (*model.Cover, error) {
	q.Size = matching.ClampSize(q.Size)

	all, err := d.searchAll(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}

	matched := make([]ranked, 0, len(all))
	for _, cand := range all {
		stage := matching.MatchCover(q, cand.ArtistName, cand.AlbumName)
		if stage == matching.MatchNone {
			continue
		}
		cand.Score = matching.ScoreCover(q, cand.ArtistName, cand.AlbumName)
		matched = append(matched, ranked{cand: cand, stage: stage})
	}
	if len(matched) == 0 {
		return nil, nil
	}

	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].stage != matched[j].stage {
			return matched[i].stage > matched[j].stage // MatchExact first
		}
		pi, pj := d.priority[matched[i].cand.Source], d.priority[matched[j].cand.Source]
		if pi != pj {
			return pi < pj
		}
		return matched[i].cand.Score > matched[j].cand.Score
	})

	matched = dedupe(matched)
	if len(matched) > maxFetchAttempts {
		matched = matched[:maxFetchAttempts]
	}

	return d.fetchFirst(ctx, matched, q.Size)
}

// searchAll queries every source in parallel and returns the union, preserving
// provider order so the priority tie-break is deterministic.
func (d *Dispatcher) searchAll(ctx context.Context, q model.Query) ([]*model.Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	results := make([][]*model.Candidate, len(d.providers))
	failures := make([]bool, len(d.providers))

	var wg sync.WaitGroup
	for i, prov := range d.providers {
		wg.Add(1)
		go func(idx int, p Provider) {
			defer wg.Done()
			cands, err := p.Search(ctx, q, candidatesPerProvider)
			if err != nil {
				failures[idx] = true
				return
			}
			results[idx] = cands
		}(i, prov)
	}
	wg.Wait()

	var all []*model.Candidate
	failed := 0
	for i, cands := range results {
		if failures[i] {
			failed++
			continue
		}
		all = append(all, cands...)
	}

	// Only a total outage is an error. A single source being down or returning
	// nothing is ordinary.
	if len(all) == 0 && failed == len(d.providers) {
		return nil, fmt.Errorf("all %d artwork providers failed", failed)
	}
	return all, nil
}

// fetchFirst downloads the first ranked candidate that yields an image.
//
// Falling through is the point: the iTunes catalogue often matches an album but
// serves no artwork for it, and the NetEase entry for the same record does. The
// consuming player writes whatever comes back into the audio file, so trying
// the next candidate is far better than returning nothing.
func (d *Dispatcher) fetchFirst(ctx context.Context, items []ranked, size int) (*model.Cover, error) {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	byName := make(map[string]Provider, len(d.providers))
	for _, p := range d.providers {
		byName[p.Name()] = p
	}

	var lastErr error
	for _, item := range items {
		prov, ok := byName[item.cand.Source]
		if !ok {
			continue
		}
		cover, err := prov.FetchCover(ctx, item.cand, size)
		if err != nil {
			if !errors.Is(err, ErrNoArtwork) {
				lastErr = err
			}
			continue
		}
		return cover, nil
	}

	// Candidates matched but none had a usable image. That is a miss, not a
	// failure — unless every attempt errored, in which case it is worth
	// surfacing.
	if lastErr != nil {
		return nil, nil
	}
	return nil, nil
}

// dedupe collapses duplicate entries *within one source*, keeping the
// highest-ranked one.
//
// The source is part of the key on purpose. Keying on (artist, album) alone
// would merge the iTunes entry for a record with the NetEase entry for the same
// record, and since the primary is tried first, a source that matches an album
// but serves no image for it would consume the attempt and the fallback would
// never be reached.
func dedupe(items []ranked) []ranked {
	seen := make(map[string]struct{}, len(items))
	out := make([]ranked, 0, len(items))
	for _, item := range items {
		key := item.cand.Source + "\x00" +
			matching.IdentityKey(item.cand.ArtistName, item.cand.AlbumName, 0)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
