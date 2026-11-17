// Command s3gateway puts the S3 object ACLs of AWS on top of Garage, which
// has none: a private bucket can hold public objects, and a public bucket
// (Garage's website access) serves every object.
//
// Every S3 request goes through it. The signed ones are forwarded to Garage
// untouched (Garage checks them) and the gateway records, once Garage
// accepted the write, which objects are public: the x-amz-acl and
// x-amz-grant-read headers of PutObject, CopyObject and
// CreateMultipartUpload. It answers GetObjectAcl and PutObjectAcl itself,
// after checking their signature with the secrets of Garage's admin API.
// The anonymous GET and HEAD requests of a public object are signed with
// the global key and forwarded; the other anonymous reads are denied.
//
// The public objects are marker files in ACL_DIR. The command only uses the
// standard library: FrankenManager builds it in a scratch image from its
// embedded sources.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// HealthPath answers 200: the dot keeps it out of the bucket names.
const HealthPath = "/.frankenmanager/health"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	cfg, err := configFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	g, err := New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           g,
		ReadHeaderTimeout: 30 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("S3 ACL gateway listening on %s", cfg.Listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// configFromEnv reads the configuration of the container.
func configFromEnv() (Config, error) {
	cfg := Config{
		Listen:     os.Getenv("LISTEN"),
		S3URL:      os.Getenv("GARAGE_S3_URL"),
		WebURL:     os.Getenv("GARAGE_WEB_URL"),
		AdminURL:   os.Getenv("GARAGE_ADMIN_URL"),
		AdminToken: os.Getenv("GARAGE_ADMIN_TOKEN"),
		AccessKey:  os.Getenv("S3_ACCESS_KEY_ID"),
		SecretKey:  os.Getenv("S3_SECRET_ACCESS_KEY"),
		Region:     os.Getenv("S3_REGION"),
		RootDomain: os.Getenv("S3_ROOT_DOMAIN"),
		ACLDir:     os.Getenv("ACL_DIR"),
	}
	if cfg.Listen == "" {
		cfg.Listen = ":3900"
	}
	if cfg.ACLDir == "" {
		cfg.ACLDir = "/acl"
	}
	for name, v := range map[string]string{
		"GARAGE_S3_URL": cfg.S3URL, "GARAGE_WEB_URL": cfg.WebURL, "GARAGE_ADMIN_URL": cfg.AdminURL,
		"GARAGE_ADMIN_TOKEN": cfg.AdminToken, "S3_ACCESS_KEY_ID": cfg.AccessKey,
		"S3_SECRET_ACCESS_KEY": cfg.SecretKey, "S3_REGION": cfg.Region,
	} {
		if v == "" {
			return cfg, fmt.Errorf("%s is not set", name)
		}
	}
	return cfg, nil
}

// healthcheck asks the running gateway for HealthPath: the image has no
// shell nor curl.
func healthcheck() int {
	port := "3900"
	if _, p, err := net.SplitHostPort(os.Getenv("LISTEN")); err == nil && p != "" {
		port = p
	}
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://127.0.0.1:" + port + HealthPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
