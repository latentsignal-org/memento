package server

import (
	"errors"
	"net/http"
	"regexp"

	"memento/backend/internal/avatar"
)

var avatarHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *Server) handleGetAvatar(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !avatarHashPattern.MatchString(hash) {
		writeError(w, http.StatusBadRequest, errors.New("invalid avatar hash"))
		return
	}
	size := avatar.ClampSize(parseIntQuery(r, "s", 64))
	initials := avatar.SanitizeInitials(r.URL.Query().Get("i"))
	img, err := s.avatars.Image(r.Context(), hash, initials, size)
	if err != nil {
		if isNotSetUp(err) {
			img = avatar.ImageResponse{
				Bytes:       avatar.FallbackSVG(hash, initials, size),
				ContentType: "image/svg+xml; charset=utf-8",
			}
		} else {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	etag := avatar.LocalETag(img.ContentType, img.Bytes)
	w.Header().Set("Content-Type", img.ContentType)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(img.Bytes)
}
