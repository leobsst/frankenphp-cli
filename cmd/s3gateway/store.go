package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Store keeps which objects are public: one marker file per public object,
// <dir>/<bucket ID>/<SHA-256 of the key>, holding the key. Absent means
// private, as on AWS. The pending ACLs of the multipart uploads are in
// <dir>/.uploads/<SHA-256 of the upload ID>. The files are written to a
// temporary name then renamed, so that a crash never leaves half a marker.
type Store struct {
	dir string
}

// uploadsDir holds the ACLs of the unfinished multipart uploads. Bucket
// IDs are hex: no clash.
const uploadsDir = ".uploads"

func hashName(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (s *Store) bucketDir(bucketID string) (string, error) {
	if bucketID == "" || strings.ContainsAny(bucketID, `/\.`) {
		return "", errors.New("invalid bucket ID")
	}
	return filepath.Join(s.dir, bucketID), nil
}

// Public reports whether the object is public.
func (s *Store) Public(bucketID, key string) (bool, error) {
	dir, err := s.bucketDir(bucketID)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(dir, hashName(key)))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Set makes the object public or private.
func (s *Store) Set(bucketID, key string, public bool) error {
	dir, err := s.bucketDir(bucketID)
	if err != nil {
		return err
	}
	return s.write(dir, hashName(key), key, public)
}

// SetPending records the ACL of a multipart upload, applied when it
// completes. Only the public ones need a file.
func (s *Store) SetPending(uploadID string, public bool) error {
	return s.write(filepath.Join(s.dir, uploadsDir), hashName(uploadID), uploadID, public)
}

// TakePending returns whether a multipart upload was created public, and
// forgets it.
func (s *Store) TakePending(uploadID string) (bool, error) {
	path := filepath.Join(s.dir, uploadsDir, hashName(uploadID))
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, remove(path)
}

// DropBucket forgets every object of a bucket.
func (s *Store) DropBucket(bucketID string) error {
	dir, err := s.bucketDir(bucketID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// write creates or removes the marker dir/name, holding content.
func (s *Store) write(dir, name, content string, present bool) error {
	path := filepath.Join(dir, name)
	if !present {
		return remove(path)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(content)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
