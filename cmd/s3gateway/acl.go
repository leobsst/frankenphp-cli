package main

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/sigv4"
)

// allUsers is the grantee of the public objects.
const allUsers = "http://acs.amazonaws.com/groups/global/AllUsers"

// cannedACLs are the values of x-amz-acl, true for the public ones.
var cannedACLs = map[string]bool{
	"private": false, "public-read": true, "public-read-write": true, "authenticated-read": false,
	"aws-exec-read": false, "bucket-owner-read": false, "bucket-owner-full-control": false,
	"log-delivery-write": false,
}

// publicFromHeaders reports whether the ACL headers of a request make the
// object readable by everyone: a public canned ACL, or a read grant to
// AllUsers.
func publicFromHeaders(h http.Header) bool {
	if cannedACLs[strings.ToLower(strings.TrimSpace(h.Get("X-Amz-Acl")))] {
		return true
	}
	for _, name := range []string{"X-Amz-Grant-Read", "X-Amz-Grant-Full-Control"} {
		for _, v := range h.Values(name) {
			if strings.Contains(v, allUsers) {
				return true
			}
		}
	}
	return false
}

// accessControlPolicy is the body of GetObjectAcl and PutObjectAcl.
type accessControlPolicy struct {
	XMLName xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ AccessControlPolicy"`
	Owner   owner    `xml:"Owner"`
	Grants  []grant  `xml:"AccessControlList>Grant"`
}

type owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName,omitempty"`
}

type grant struct {
	Grantee    grantee `xml:"Grantee"`
	Permission string  `xml:"Permission"`
}

type grantee struct {
	XMLNS string `xml:"xmlns:xsi,attr,omitempty"`
	Type  string `xml:"xsi:type,attr,omitempty"`
	ID    string `xml:"ID,omitempty"`
	URI   string `xml:"URI,omitempty"`
}

// policyIsPublic reports whether a PutObjectAcl body grants READ (or full
// control) to AllUsers.
func policyIsPublic(body []byte) (bool, error) {
	var p struct {
		Grants []struct {
			URI        string `xml:"Grantee>URI"`
			Permission string `xml:"Permission"`
		} `xml:"AccessControlList>Grant"`
	}
	if err := xml.Unmarshal(body, &p); err != nil {
		return false, err
	}
	for _, g := range p.Grants {
		if g.URI == allUsers && (g.Permission == "READ" || g.Permission == "FULL_CONTROL") {
			return true, nil
		}
	}
	return false, nil
}

// authorize checks the signature of an ACL request and the permission of
// its key on the bucket. It answers the request and returns false when
// denied.
func (g *Gateway) authorize(w http.ResponseWriter, r *http.Request, t target, body []byte,
	allowed func(Perms) bool) (BucketInfo, bool) {
	var key KeyInfo
	_, err := sigv4.Verify(r, body, g.cfg.Region, func(id string) (string, error) {
		k, err := g.admin.Key(r.Context(), id)
		key = k
		return k.Secret, err
	}, g.now())
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, r, http.StatusForbidden, "InvalidAccessKeyId", "The access key ID does not exist.", t.resource())
		return BucketInfo{}, false
	case err != nil:
		writeError(w, r, http.StatusForbidden, "SignatureDoesNotMatch", err.Error(), t.resource())
		return BucketInfo{}, false
	}
	bucket, err := g.admin.Bucket(r.Context(), t.bucket)
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, r, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", t.resource())
		return bucket, false
	case err != nil:
		log.Printf("bucket %s: %v", t.bucket, err)
		writeError(w, r, http.StatusServiceUnavailable, "ServiceUnavailable", "The S3 server is not ready.", t.resource())
		return bucket, false
	case !allowed(key.On(bucket.ID)):
		writeError(w, r, http.StatusForbidden, "AccessDenied", "Access Denied", t.resource())
		return bucket, false
	}
	exists, err := g.objectExists(r.Context(), t)
	switch {
	case err != nil:
		log.Printf("object %s: %v", t.resource(), err)
		writeError(w, r, http.StatusServiceUnavailable, "ServiceUnavailable", "The S3 server is not ready.", t.resource())
		return bucket, false
	case !exists:
		writeError(w, r, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", t.resource())
		return bucket, false
	}
	return bucket, true
}

// getObjectACL answers GetObjectAcl: full control to the owner, and READ
// to AllUsers for a public object (or any object of a public bucket).
func (g *Gateway) getObjectACL(w http.ResponseWriter, r *http.Request, t target) {
	body, ok := readBody(w, r, t)
	if !ok {
		return
	}
	bucket, ok := g.authorize(w, r, t, body, func(p Perms) bool { return p.Read || p.Owner })
	if !ok {
		return
	}
	public := bucket.WebsiteAccess
	if !public {
		var err error
		if public, err = g.store.Public(bucket.ID, t.key); err != nil {
			log.Printf("ACL of %s: %v", t.resource(), err)
			writeError(w, r, http.StatusInternalServerError, "InternalError", "The ACL could not be read.", t.resource())
			return
		}
	}
	const xsi = "http://www.w3.org/2001/XMLSchema-instance"
	o := owner{ID: "frankenmanager", DisplayName: "frankenmanager"}
	p := accessControlPolicy{Owner: o, Grants: []grant{{
		Grantee:    grantee{XMLNS: xsi, Type: "CanonicalUser", ID: o.ID},
		Permission: "FULL_CONTROL",
	}}}
	if public {
		p.Grants = append(p.Grants, grant{Grantee: grantee{XMLNS: xsi, Type: "Group", URI: allUsers}, Permission: "READ"})
	}
	data, err := xml.Marshal(p)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "InternalError", err.Error(), t.resource())
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Length", strconv.Itoa(len(xml.Header)+len(data)))
	_, _ = io.WriteString(w, xml.Header)
	_, _ = w.Write(data)
}

// putObjectACL answers PutObjectAcl, from the x-amz-acl or x-amz-grant-*
// headers or from the AccessControlPolicy body.
func (g *Gateway) putObjectACL(w http.ResponseWriter, r *http.Request, t target) {
	body, ok := readBody(w, r, t)
	if !ok {
		return
	}
	canned := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Amz-Acl")))
	if _, known := cannedACLs[canned]; canned != "" && !known {
		writeError(w, r, http.StatusBadRequest, "InvalidArgument", "Unknown canned ACL "+canned+".", t.resource())
		return
	}
	bucket, ok := g.authorize(w, r, t, body, func(p Perms) bool { return p.Write || p.Owner })
	if !ok {
		return
	}
	public := publicFromHeaders(r.Header)
	if canned == "" && r.Header.Get("X-Amz-Grant-Read") == "" && r.Header.Get("X-Amz-Grant-Full-Control") == "" {
		decoded, err := decodedBody(r, body)
		if err == nil && len(bytes.TrimSpace(decoded)) > 0 {
			public, err = policyIsPublic(decoded)
		}
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "MalformedACLError", "The ACL is not well formed.", t.resource())
			return
		}
	}
	if err := g.store.Set(bucket.ID, t.key, public); err != nil {
		log.Printf("ACL of %s: %v", t.resource(), err)
		writeError(w, r, http.StatusInternalServerError, "InternalError", "The ACL could not be saved.", t.resource())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// readBody reads the small body of an ACL request.
func readBody(w http.ResponseWriter, r *http.Request, t target) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "IncompleteBody", "The request body could not be read.", t.resource())
		return nil, false
	}
	return body, true
}

// decodedBody returns the payload of a body, aws-chunked decoded when the
// SDK streamed it (STREAMING-* payload hash): <hex size>[;ext]\r\n<data>\r\n
// chunks, up to an empty one, then the trailers.
func decodedBody(r *http.Request, raw []byte) ([]byte, error) {
	if !strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") &&
		!strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
		return raw, nil
	}
	var out bytes.Buffer
	br := bufio.NewReader(bytes.NewReader(raw))
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		sizeHex, _, _ := strings.Cut(strings.TrimSpace(line), ";")
		size, err := strconv.ParseInt(sizeHex, 16, 64)
		if err != nil || size < 0 || size > int64(len(raw)) {
			return nil, errors.New("malformed aws-chunked body")
		}
		if size == 0 {
			return out.Bytes(), nil
		}
		if _, err := io.CopyN(&out, br, size); err != nil {
			return nil, err
		}
		if _, err := br.Discard(2); err != nil {
			return nil, err
		}
	}
}
