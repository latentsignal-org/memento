package avatar

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const knownHashCacheTTL = time.Minute

type ImageResponse struct {
	Bytes       []byte
	ContentType string
}

type Service struct {
	DB       *sql.DB
	Fetcher  *Fetcher
	group    singleflight.Group
	outbound chan struct{}

	knownMu       sync.Mutex
	knownHashes   map[string]bool
	knownHashesAt time.Time
}

func NewService(db *sql.DB, fetcher *Fetcher) *Service {
	if fetcher == nil {
		fetcher = DefaultFetcher()
	}
	return &Service{
		DB:       db,
		Fetcher:  fetcher,
		outbound: make(chan struct{}, 4),
	}
}

func (s *Service) Image(ctx context.Context, hash string, initials string, size int) (ImageResponse, error) {
	if row, ok, err := Get(ctx, s.DB, hash); err != nil {
		return ImageResponse{}, err
	} else if ok {
		return responseFromRow(row, hash, initials, size), nil
	}

	known, err := s.knownHash(ctx, hash)
	if err != nil {
		return ImageResponse{}, err
	}
	if !known {
		return fallbackResponse(hash, initials, size), nil
	}

	v, err, _ := s.group.Do(hash, func() (any, error) {
		if row, ok, err := Get(ctx, s.DB, hash); err != nil {
			return nil, err
		} else if ok {
			return row, nil
		}
		s.outbound <- struct{}{}
		defer func() { <-s.outbound }()
		result, err := s.Fetcher.Fetch(ctx, hash)
		if err != nil {
			return Row{EmailHash: hash, Status: StatusNotFound}, nil
		}
		row := Row{
			EmailHash:    hash,
			Status:       result.Status,
			Image:        result.Image,
			MimeType:     result.MimeType,
			ByteSize:     result.ByteSize,
			UpstreamETag: result.UpstreamETag,
		}
		if err := Put(ctx, s.DB, row); err != nil {
			return nil, err
		}
		return row, nil
	})
	if err != nil {
		return ImageResponse{}, err
	}
	return responseFromRow(v.(Row), hash, initials, size), nil
}

func (s *Service) knownHash(ctx context.Context, hash string) (bool, error) {
	now := time.Now()
	s.knownMu.Lock()
	if s.knownHashes != nil && now.Sub(s.knownHashesAt) < knownHashCacheTTL {
		known := s.knownHashes[hash]
		s.knownMu.Unlock()
		return known, nil
	}
	s.knownMu.Unlock()

	known, err := KnownHashes(ctx, s.DB)
	if err != nil {
		return false, err
	}
	next := make(map[string]bool, len(known))
	for _, item := range known {
		next[item.EmailHash] = true
	}

	s.knownMu.Lock()
	s.knownHashes = next
	s.knownHashesAt = now
	result := s.knownHashes[hash]
	s.knownMu.Unlock()
	return result, nil
}

func responseFromRow(row Row, hash string, initials string, size int) ImageResponse {
	if row.Status == StatusFound {
		return ImageResponse{Bytes: row.Image, ContentType: row.MimeType}
	}
	return fallbackResponse(hash, initials, size)
}

func fallbackResponse(hash string, initials string, size int) ImageResponse {
	return ImageResponse{
		Bytes:       FallbackSVG(hash, initials, size),
		ContentType: "image/svg+xml; charset=utf-8",
	}
}

func LocalETag(contentType string, body []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(contentType))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(body)
	return fmt.Sprintf(`"%s"`, hex.EncodeToString(h.Sum(nil)))
}
