// Package sigv4 signs and verifies S3 requests with AWS Signature Version 4.
//
// Standard library only: the S3 ACL gateway, built in its own container
// from the embedded sources, imports it.
package sigv4

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	algorithm = "AWS4-HMAC-SHA256"
	// UnsignedPayload is the payload hash of the requests whose body is not
	// signed (presigned URLs, some SDK uploads).
	UnsignedPayload = "UNSIGNED-PAYLOAD"
	// MaxSkew is the largest difference accepted between X-Amz-Date and
	// the clock.
	MaxSkew    = 15 * time.Minute
	dateFormat = "20060102T150405Z"
)

// Credentials sign requests.
type Credentials struct {
	AccessKey, SecretKey, Region string
}

// Sign adds the X-Amz-Date, X-Amz-Content-Sha256 and Authorization headers
// to req, for the S3 service. uri and rawQuery must already be in canonical
// form (see EncodePath and CanonicalQuery), payloadHash is the hex SHA-256
// of the body or UnsignedPayload. Content-MD5 and every X-Amz-* header are
// signed, as S3 requires.
func Sign(req *http.Request, c Credentials, uri, rawQuery, payloadHash string, now time.Time) {
	now = now.UTC()
	amzDate, day := now.Format(dateFormat), now.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	req.Header.Del("Authorization")

	names := []string{"host"}
	values := map[string]string{"host": req.URL.Host}
	for name := range req.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-amz-") || lower == "content-md5" {
			names = append(names, lower)
			values[lower] = headerValue(req, lower)
		}
	}
	sort.Strings(names)
	var headers strings.Builder
	for _, n := range names {
		headers.WriteString(n + ":" + values[n] + "\n")
	}
	signed := strings.Join(names, ";")

	canonical := strings.Join([]string{req.Method, uri, rawQuery, headers.String(), signed, payloadHash}, "\n")
	scope := day + "/" + c.Region + "/s3/aws4_request"
	signature := signatureOf(c.SecretKey, day, c.Region, amzDate, scope, canonical)
	req.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, c.AccessKey, scope, signed, signature))
}

func signatureOf(secret, day, region, amzDate, scope, canonical string) string {
	toSign := strings.Join([]string{algorithm, amzDate, scope, SHA256Hex([]byte(canonical))}, "\n")
	key := hmacSHA256([]byte("AWS4"+secret), day)
	for _, part := range []string{region, "s3", "aws4_request"} {
		key = hmacSHA256(key, part)
	}
	return hex.EncodeToString(hmacSHA256(key, toSign))
}

// SHA256Hex returns the hex SHA-256 of b.
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// EncodePath URI-encodes each segment of a path the SigV4 way.
func EncodePath(p string) string {
	segments := strings.Split(p, "/")
	for i, s := range segments {
		segments[i] = URIEncode(s)
	}
	return strings.Join(segments, "/")
}

// CanonicalQuery is the query sorted by key then value, encoded the SigV4
// way.
func CanonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		values := append([]string(nil), q[k]...)
		sort.Strings(values)
		for _, v := range values {
			parts = append(parts, URIEncode(k)+"="+URIEncode(v))
		}
	}
	return strings.Join(parts, "&")
}

// URIEncode encodes every byte but the unreserved characters A-Z a-z 0-9
// - _ . ~ (RFC 3986), as SigV4 requires.
func URIEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || strings.IndexByte("-_.~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// ErrNoAuth reports a request with neither an Authorization header nor a
// presigned query.
var ErrNoAuth = errors.New("anonymous request")

// SecretFunc returns the secret key of an access key ID.
type SecretFunc func(accessKey string) (string, error)

// auth is a parsed signature, from the header or the query.
type auth struct {
	accessKey, day, region, service string
	signedHeaders                   []string
	signature                       string
	amzDate                         string
	presigned                       bool
	expires                         time.Duration
}

// Verify checks the signature of r, whose body was read into body, and
// returns the access key ID that signed it. region is the region of the
// server. The streaming payloads (aws-chunked) are accepted on the seed
// signature: their chunk signatures are not checked.
func Verify(r *http.Request, body []byte, region string, secret SecretFunc, now time.Time) (string, error) {
	a, err := parseAuth(r)
	if err != nil {
		return "", err
	}
	if a.region != region || a.service != "s3" {
		return a.accessKey, fmt.Errorf("unexpected credential scope %s/%s", a.region, a.service)
	}
	date, err := time.Parse(dateFormat, a.amzDate)
	if err != nil || !strings.HasPrefix(a.amzDate, a.day) {
		return a.accessKey, errors.New("invalid X-Amz-Date")
	}
	if a.presigned {
		if now.After(date.Add(a.expires)) || date.After(now.Add(MaxSkew)) {
			return a.accessKey, errors.New("request has expired")
		}
	} else if d := now.Sub(date); d > MaxSkew || d < -MaxSkew {
		return a.accessKey, errors.New("request time too skewed")
	}
	// The access control headers must be signed, else anyone could change
	// them on a signed request
	for name := range r.Header {
		lower := strings.ToLower(name)
		if (lower == "x-amz-acl" || strings.HasPrefix(lower, "x-amz-grant-")) && !slices.Contains(a.signedHeaders, lower) {
			return a.accessKey, fmt.Errorf("header %s is not signed", lower)
		}
	}

	payload := UnsignedPayload
	if !a.presigned {
		payload = r.Header.Get("X-Amz-Content-Sha256")
		switch {
		case payload == "":
			return a.accessKey, errors.New("missing X-Amz-Content-Sha256")
		case payload == UnsignedPayload || strings.HasPrefix(payload, "STREAMING-"):
		case payload != SHA256Hex(body):
			return a.accessKey, errors.New("payload hash mismatch")
		}
	}

	query := r.URL.Query()
	query.Del("X-Amz-Signature")
	var headers strings.Builder
	for _, name := range a.signedHeaders {
		headers.WriteString(name + ":" + headerValue(r, name) + "\n")
	}
	canonical := strings.Join([]string{r.Method, EncodePath(r.URL.Path), CanonicalQuery(query),
		headers.String(), strings.Join(a.signedHeaders, ";"), payload}, "\n")

	key, err := secret(a.accessKey)
	if err != nil {
		return a.accessKey, err
	}
	scope := a.day + "/" + a.region + "/s3/aws4_request"
	want := signatureOf(key, a.day, a.region, a.amzDate, scope, canonical)
	if !hmac.Equal([]byte(want), []byte(a.signature)) {
		return a.accessKey, errors.New("signature does not match")
	}
	return a.accessKey, nil
}

// headerValue is the canonical value of a signed header: the values
// joined with commas, trimmed, inner spaces collapsed.
func headerValue(r *http.Request, name string) string {
	var values []string
	switch name {
	case "host":
		values = []string{r.Host}
	case "content-length":
		values = []string{strconv.FormatInt(r.ContentLength, 10)}
	case "transfer-encoding":
		values = r.TransferEncoding
	default:
		values = r.Header.Values(name)
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strings.Join(strings.Fields(v), " ")
	}
	return strings.Join(out, ",")
}

// parseAuth reads the signature of the Authorization header or, failing
// that, of the presigned query.
func parseAuth(r *http.Request) (auth, error) {
	var a auth
	var credential, signed string
	if h := r.Header.Get("Authorization"); h != "" {
		rest, ok := strings.CutPrefix(h, algorithm+" ")
		if !ok {
			return a, errors.New("unsupported authorization scheme")
		}
		for _, part := range strings.Split(rest, ",") {
			k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
			switch k {
			case "Credential":
				credential = v
			case "SignedHeaders":
				signed = v
			case "Signature":
				a.signature = v
			}
		}
		a.amzDate = r.Header.Get("X-Amz-Date")
	} else {
		q := r.URL.Query()
		if q.Get("X-Amz-Signature") == "" {
			return a, ErrNoAuth
		}
		if q.Get("X-Amz-Algorithm") != algorithm {
			return a, errors.New("unsupported X-Amz-Algorithm")
		}
		credential, signed, a.signature = q.Get("X-Amz-Credential"), q.Get("X-Amz-SignedHeaders"), q.Get("X-Amz-Signature")
		a.amzDate, a.presigned = q.Get("X-Amz-Date"), true
		seconds, err := strconv.Atoi(q.Get("X-Amz-Expires"))
		if err != nil || seconds < 1 || seconds > 7*24*3600 {
			return a, errors.New("invalid X-Amz-Expires")
		}
		a.expires = time.Duration(seconds) * time.Second
	}
	parts := strings.Split(credential, "/")
	if len(parts) != 5 || parts[4] != "aws4_request" || signed == "" || a.signature == "" {
		return a, errors.New("malformed signature")
	}
	a.accessKey, a.day, a.region, a.service = parts[0], parts[1], parts[2], parts[3]
	a.signedHeaders = strings.Split(signed, ";")
	if !sort.StringsAreSorted(a.signedHeaders) {
		return a, errors.New("malformed SignedHeaders")
	}
	return a, nil
}

// Anonymous reports whether r carries no credentials: neither an
// Authorization header nor a presigned query.
func Anonymous(r *http.Request) bool {
	if r.Header.Get("Authorization") != "" {
		return false
	}
	q := r.URL.Query()
	return q.Get("X-Amz-Signature") == "" && q.Get("X-Amz-Credential") == ""
}
