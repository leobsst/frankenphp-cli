package sigv4

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

var example = Credentials{
	AccessKey: "AKIAIOSFODNN7EXAMPLE",
	SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	Region:    "us-east-1",
}

var exampleTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)

func secretOf(id string) (string, error) { return example.SecretKey, nil }

// The "GET Object" example of the AWS Signature Version 4 documentation,
// with its Range header.
func TestVerifyAcceptsTheAWSExample(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	req.Header.Set("X-Amz-Date", "20130524T000000Z")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,"+
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date,"+
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41")
	if _, err := Verify(req, nil, "us-east-1", secretOf, exampleTime); err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-10")
	if _, err := Verify(req, nil, "us-east-1", secretOf, exampleTime); err == nil {
		t.Error("changed signed header accepted")
	}
}

// The presigned URL example of the AWS documentation.
func TestVerifyAcceptsTheAWSPresignedExample(t *testing.T) {
	u := "https://examplebucket.s3.amazonaws.com/test.txt?X-Amz-Algorithm=AWS4-HMAC-SHA256" +
		"&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request" +
		"&X-Amz-Date=20130524T000000Z&X-Amz-Expires=86400&X-Amz-SignedHeaders=host" +
		"&X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	if _, err := Verify(req, nil, "us-east-1", secretOf, exampleTime.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(req, nil, "us-east-1", secretOf, exampleTime.Add(25*time.Hour)); err == nil {
		t.Error("expired URL accepted")
	}
}

func TestSignThenVerify(t *testing.T) {
	body := []byte("<AccessControlPolicy/>")
	req, _ := http.NewRequest(http.MethodPut, "http://127.0.0.1:3900/media/dir/a%20b+c.txt?acl", strings.NewReader(string(body)))
	now := time.Now()
	Sign(req, example, EncodePath(req.URL.Path), CanonicalQuery(url.Values{"acl": {""}}), SHA256Hex(body), now)
	req.Host = req.URL.Host
	if _, err := Verify(req, body, "us-east-1", secretOf, now); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(req, []byte("tampered"), "us-east-1", secretOf, now); err == nil {
		t.Error("tampered body accepted")
	}
	if _, err := Verify(req, body, "us-east-1", secretOf, now.Add(time.Hour)); err == nil {
		t.Error("old request accepted")
	}
}

// An ACL header added to a signed request must not be trusted.
func TestVerifyRejectsUnsignedACLHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPut, "http://127.0.0.1:3900/media/a.txt?acl", nil)
	now := time.Now()
	Sign(req, example, EncodePath(req.URL.Path), CanonicalQuery(url.Values{"acl": {""}}), SHA256Hex(nil), now)
	req.Host = req.URL.Host
	if _, err := Verify(req, nil, "us-east-1", secretOf, now); err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Amz-Acl", "public-read")
	if _, err := Verify(req, nil, "us-east-1", secretOf, now); err == nil {
		t.Error("unsigned x-amz-acl accepted")
	}
}
