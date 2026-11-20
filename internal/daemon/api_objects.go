package daemon

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/core/buckets"
	"github.com/leobsst/frankenphp-cli/internal/core/events"
	"github.com/leobsst/frankenphp-cli/internal/core/s3"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// --- S3 objects --------------------------------------------------------------

// maxUpload bounds an uploaded object.
const maxUpload = 5 << 30

// withBuckets opens the S3 service for a request that is not an operation
// (reads, uploads, downloads: they do not touch the state of the stack).
func (s *Server) withBuckets(w http.ResponseWriter, r *http.Request, fn func(svc *buckets.Service)) {
	ws, err := s.Backend(events.NewLog(nil))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = ws.Close() }()
	svc, err := ws.Buckets(r.Context())
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	fn(svc)
}

// objectKey reads the required ?key= of an object route.
func objectKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.URL.Query().Get("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, i18n.Tf("Missing field '%s'.", "key"))
		return "", false
	}
	return key, true
}

// listObjects lists a bucket one folder deep (?prefix=, ?token=).
func (s *Server) listObjects(w http.ResponseWriter, r *http.Request) {
	s.withBuckets(w, r, func(svc *buckets.Service) {
		q := r.URL.Query()
		page, err := svc.Objects(r.Context(), r.PathValue("bucket"), q.Get("prefix"), q.Get("token"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
}

// uploadObject stores the request body as ?key= (?public=true for
// public-read), its Content-Type kept.
func (s *Server) uploadObject(w http.ResponseWriter, r *http.Request) {
	key, ok := objectKey(w, r)
	if !ok {
		return
	}
	s.withBuckets(w, r, func(svc *buckets.Service) {
		body := http.MaxBytesReader(w, r.Body, maxUpload)
		info, err := svc.Upload(r.Context(), r.PathValue("bucket"), key, r.Header.Get("Content-Type"), body,
			r.ContentLength, r.URL.Query().Get("public") == "true")
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		s.Bus.Publish("s3.objects", map[string]string{"bucket": r.PathValue("bucket")})
		writeJSON(w, http.StatusCreated, info)
	})
}

// downloadObject sends an object as an attachment.
func (s *Server) downloadObject(w http.ResponseWriter, r *http.Request) {
	key, ok := objectKey(w, r)
	if !ok {
		return
	}
	s.withBuckets(w, r, func(svc *buckets.Service) {
		res, err := svc.Download(r.Context(), r.PathValue("bucket"), key)
		if err != nil {
			status := http.StatusBadGateway
			var e *s3.ObjectError
			if errors.As(err, &e) && e.Status == http.StatusNotFound {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		defer func() { _ = res.Body.Close() }()
		for _, h := range []string{"Content-Type", "Content-Length", "Last-Modified", "ETag"} {
			if v := res.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(key)}))
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, res.Body)
	})
}

// objectLink returns the public URL of an object, or a temporary one valid
// ?expires= seconds (1 hour by default, 7 days at most).
func (s *Server) objectLink(w http.ResponseWriter, r *http.Request) {
	key, ok := objectKey(w, r)
	if !ok {
		return
	}
	expires := time.Hour
	if v := r.URL.Query().Get("expires"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || time.Duration(n)*time.Second > buckets.MaxLinkDuration {
			writeError(w, http.StatusBadRequest, i18n.Tf("Invalid value '%s' for %s.", v, "expires"))
			return
		}
		expires = time.Duration(n) * time.Second
	}
	s.withBuckets(w, r, func(svc *buckets.Service) {
		link, err := svc.ObjectLink(r.Context(), r.PathValue("bucket"), key, expires)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, link)
	})
}

// objectsRequest is a request on several objects of a bucket.
type objectsRequest struct {
	Keys   []string `json:"keys"`
	Public bool     `json:"public"`
}

func readObjects(w http.ResponseWriter, r *http.Request) (objectsRequest, bool) {
	var req objectsRequest
	if !readJSON(w, r, &req) {
		return req, false
	}
	if len(req.Keys) == 0 {
		writeError(w, http.StatusBadRequest, i18n.Tf("Missing field '%s'.", "keys"))
		return req, false
	}
	return req, true
}

// deleteObjects deletes objects of a bucket.
func (s *Server) deleteObjects(w http.ResponseWriter, r *http.Request) {
	req, ok := readObjects(w, r)
	if !ok {
		return
	}
	bucket := r.PathValue("bucket")
	s.submitS3(w, r, "s3.objects.delete", func(ctx context.Context, svc *buckets.Service) (any, error) {
		return nil, svc.DeleteObjects(ctx, bucket, req.Keys)
	})
}

// setObjectsPublic makes objects public-read or private.
func (s *Server) setObjectsPublic(w http.ResponseWriter, r *http.Request) {
	req, ok := readObjects(w, r)
	if !ok {
		return
	}
	bucket := r.PathValue("bucket")
	s.submitS3(w, r, "s3.objects.public", func(ctx context.Context, svc *buckets.Service) (any, error) {
		return nil, svc.SetObjectsPublic(ctx, bucket, req.Keys, req.Public)
	})
}
