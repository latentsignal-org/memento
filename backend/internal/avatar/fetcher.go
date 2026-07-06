package avatar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxAvatarBytes int64 = 5 * 1024 * 1024

var ErrTransient = errors.New("transient avatar fetch error")

type Fetcher struct {
	BaseURL string
	Client  *http.Client
}

func DefaultFetcher() *Fetcher {
	return &Fetcher{
		BaseURL: "https://www.gravatar.com/avatar/",
		Client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (f *Fetcher) Fetch(ctx context.Context, hash string) (FetchResult, error) {
	base := f.BaseURL
	if base == "" {
		base = "https://www.gravatar.com/avatar/"
	}
	u, err := url.Parse(base)
	if err != nil {
		return FetchResult{}, fmt.Errorf("%w: invalid base URL: %v", ErrTransient, err)
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	u.Path += hash
	q := u.Query()
	q.Set("s", "256")
	q.Set("d", "404")
	q.Set("r", "g")
	u.RawQuery = q.Encode()

	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return FetchResult{}, fmt.Errorf("%w: request: %v", ErrTransient, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return FetchResult{}, fmt.Errorf("%w: request failed: %v", ErrTransient, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return FetchResult{Status: StatusNotFound}, nil
	default:
		return FetchResult{}, fmt.Errorf("%w: upstream status %d", ErrTransient, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarBytes+1))
	if err != nil {
		return FetchResult{}, fmt.Errorf("%w: read body: %v", ErrTransient, err)
	}
	if int64(len(body)) > maxAvatarBytes {
		return FetchResult{}, fmt.Errorf("%w: avatar body exceeds %d bytes", ErrTransient, maxAvatarBytes)
	}
	if len(body) == 0 {
		return FetchResult{}, fmt.Errorf("%w: empty avatar body", ErrTransient)
	}
	contentType := resp.Header.Get("Content-Type")
	mimeType := normalizeImageMime(contentType, body)
	if !strings.HasPrefix(mimeType, "image/") {
		return FetchResult{}, fmt.Errorf("%w: non-image content type %q", ErrTransient, contentType)
	}
	return FetchResult{
		Status:       StatusFound,
		Image:        body,
		MimeType:     mimeType,
		ByteSize:     int64(len(body)),
		UpstreamETag: resp.Header.Get("ETag"),
	}, nil
}

func normalizeImageMime(contentType string, body []byte) string {
	if contentType != "" {
		if mt, _, err := mime.ParseMediaType(contentType); err == nil {
			return mt
		}
	}
	detected := http.DetectContentType(body)
	if strings.HasPrefix(detected, "image/") {
		if mt, _, err := mime.ParseMediaType(detected); err == nil {
			return mt
		}
	}
	return ""
}
