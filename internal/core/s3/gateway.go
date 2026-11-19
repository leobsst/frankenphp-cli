package s3

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// The ACL gateway (cmd/s3gateway) serves the S3 API in front of Garage:
// it records which objects are public (x-amz-acl, PutObjectAcl) and lets
// the anonymous requests read them, as AWS does. FrankenManager builds its
// image from the embedded sources.
const (
	GatewayContainer = "franken_s3_gateway"
	// GatewayService is the compose service of the gateway.
	GatewayService = "s3-gateway"
	// GatewayImage is built locally, labeled with the hash of its sources.
	GatewayImage = "frankenmanager-s3-gateway:local"
	// GatewayContextPath is the build context, relative to the data
	// directory.
	GatewayContextPath = "garage/gateway"
	// gatewayDockerfile is the Dockerfile in the embedded sources.
	gatewayDockerfile = "docker/s3-gateway.Dockerfile"
	// aclDir holds the public object markers, in S3_DIR.
	aclDir = "acl"

	// GaragePortKey is the port of Garage's S3 API behind the gateway, in
	// production (host network, on 127.0.0.1).
	GaragePortKey     = "S3_GARAGE_PORT"
	DefaultGaragePort = "3904"
)

// GaragePort returns S3_GARAGE_PORT, DefaultGaragePort when empty.
func GaragePort(c Getter) string { return getOr(c, GaragePortKey, DefaultGaragePort) }

// gatewayGoMod is the go.mod of the build context: the gateway only
// imports the standard library and internal/sigv4.
const gatewayGoMod = "module github.com/leobsst/frankenphp-cli\n\ngo 1.26\n"

// WriteGatewayContext writes the build context of the gateway image to
// <dataDir>/GatewayContextPath from sources (the embedded files: the
// Dockerfile, cmd/s3gateway and internal/sigv4), test files left out. It
// returns the context directory and the hash of its content, which labels
// the image so that it is rebuilt when the sources change.
func WriteGatewayContext(sources fs.FS, dataDir string) (string, string, error) {
	dir := filepath.Join(dataDir, filepath.FromSlash(GatewayContextPath))
	// Files of an older gateway must not linger
	if err := os.RemoveAll(dir); err != nil {
		return "", "", err
	}
	files := map[string][]byte{"go.mod": []byte(gatewayGoMod)}
	err := fs.WalkDir(sources, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, "_test.go") {
			return err
		}
		data, err := fs.ReadFile(sources, p)
		if err != nil {
			return err
		}
		if p == gatewayDockerfile {
			p = "Dockerfile"
		}
		files[p] = data
		return nil
	})
	if err != nil {
		return "", "", err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", "", err
		}
		if err := os.WriteFile(target, files[name], 0o644); err != nil {
			return "", "", err
		}
		h.Write([]byte(path.Clean(name) + "\x00"))
		h.Write(files[name])
		h.Write([]byte{0})
	}
	return dir, hex.EncodeToString(h.Sum(nil)), nil
}

// gatewayService returns the YAML lines of the gateway compose service:
// it publishes S3_PORT in development and listens on it in production
// (host network, on BIND_ADDRESS), Garage's API staying behind it.
func gatewayService(production bool) []string {
	dir := "${" + DirKey + ":-./s3}"
	garage, web, admin := "http://"+ComposeService+":"+DefaultPort, "http://"+ComposeService+":"+DefaultWebPort,
		"http://"+ComposeService+":"+DefaultAdminPort
	listen := ":" + DefaultPort
	if production {
		dir = "${" + DirKey + ":-${PWD}/s3}"
		garage = "http://127.0.0.1:${" + GaragePortKey + ":-" + DefaultGaragePort + "}"
		web = "http://127.0.0.1:${" + WebPortKey + ":-" + DefaultWebPort + "}"
		admin = "http://127.0.0.1:${" + AdminPortKey + ":-" + DefaultAdminPort + "}"
		listen = "${BIND_ADDRESS:-127.0.0.1}:${" + PortKey + ":-" + DefaultPort + "}"
	}
	lines := []string{
		"  " + GatewayService + ":",
		"    container_name: " + GatewayContainer,
		"    image: " + GatewayImage,
		"    pull_policy: never",
		"    restart: always",
		"    user: ${UID}:${GID}",
		"    depends_on:",
		"      " + ComposeService + ":",
		"        condition: service_healthy",
		"    environment:",
		"      LISTEN: \"" + listen + "\"",
		"      GARAGE_S3_URL: " + garage,
		"      GARAGE_WEB_URL: " + web,
		"      GARAGE_ADMIN_URL: " + admin,
		"      GARAGE_ADMIN_TOKEN: ${" + AdminTokenKey + "}",
		"      S3_ACCESS_KEY_ID: ${" + AccessKeyKey + "}",
		"      S3_SECRET_ACCESS_KEY: ${" + SecretKeyKey + "}",
		"      S3_REGION: ${" + RegionKey + ":-" + DefaultRegion + "}",
		"      S3_ROOT_DOMAIN: " + Domain,
		"      ACL_DIR: /acl",
		"    volumes:",
		"      - " + dir + "/" + aclDir + ":/acl",
	}
	if production {
		lines = append(lines, "    network_mode: host")
	} else {
		lines = append(lines,
			"    ports:",
			"      - ${S3_PUBLISH:-127.0.0.1:"+DefaultPort+":"+DefaultPort+"}",
			"    networks:",
			"      - franken_network",
		)
	}
	return append(lines,
		"    healthcheck:",
		`      test: ["CMD", "/s3gateway", "healthcheck"]`,
		"      interval: 10s",
		"      timeout: 5s",
		"      retries: 5",
		"      start_period: 5s",
		"",
	)
}

// ACLDir is the directory of the public object markers, in the objects
// directory dir (S3_DIR resolved).
func ACLDir(dir string) string { return filepath.Join(dir, aclDir) }

// PublicObjects counts the objects of a bucket made public by their ACL.
func PublicObjects(dir, bucketID string) int {
	entries, err := os.ReadDir(filepath.Join(ACLDir(dir), bucketID))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			n++
		}
	}
	return n
}

// DropBucketACL forgets the public objects of a deleted bucket.
func DropBucketACL(dir, bucketID string) error {
	if bucketID == "" || strings.ContainsAny(bucketID, `/\.`) {
		return nil
	}
	return os.RemoveAll(filepath.Join(ACLDir(dir), bucketID))
}
