package s3

import (
	"os"
	"path/filepath"
)

const (
	// AWSImage runs the AWS CLI for `s3 aws`.
	AWSImage = "amazon/aws-cli:2.31.0"
	// AWSConfigPath is the AWS CLI configuration of `s3 aws`, relative to
	// the data directory: it holds the global credentials (mode 0600).
	AWSConfigPath = "garage/aws-config"
	// awsConfigMount is where the container reads it.
	awsConfigMount = "/frankenmanager/aws-config"
)

// AppEndpoint is the endpoint the PHP apps should use: the HTTPS host in
// development, which the PHP containers resolve and trust (mkcert CA), so
// that presigned URLs also work in browsers and on other devices. The
// reverse proxy does not serve it in production: the API port there.
func AppEndpoint(c Getter, production bool) string {
	if production {
		return Endpoint(c)
	}
	return "https://" + Domain
}

// GarageExecArgs returns the docker arguments running Garage's own CLI in
// its container, with args.
func GarageExecArgs(args []string, tty bool) []string {
	out := []string{"exec", "-i"}
	if tty {
		out = append(out, "-t")
	}
	return append(append(out, ContainerName, "/garage"), args...)
}

// AWSConfig returns the AWS CLI configuration of the global key: the S3
// API without TLS (host network), path style, and checksums only where
// S3 requires them.
func AWSConfig(c Getter) string {
	return "[default]\n" +
		"aws_access_key_id = " + c.Get(AccessKeyKey) + "\n" +
		"aws_secret_access_key = " + c.Get(SecretKeyKey) + "\n" +
		"region = " + Region(c) + "\n" +
		"endpoint_url = " + Endpoint(c) + "\n" +
		"request_checksum_calculation = when_required\n" +
		"response_checksum_validation = when_required\n" +
		"s3 =\n" +
		"  addressing_style = path\n"
}

// WriteAWSConfig writes <dataDir>/garage/aws-config, readable by the user
// only, and returns its path.
func WriteAWSConfig(dataDir string, c Getter) (string, error) {
	path := filepath.Join(dataDir, AWSConfigPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(AWSConfig(c)), 0o600); err != nil {
		return "", err
	}
	// WriteFile keeps the mode of an existing file
	return path, os.Chmod(path, 0o600)
}

// AWSRunArgs returns the docker arguments running the AWS CLI with args,
// as user, in a one-off container on the host network: configFile is
// mounted read-only, and workdir (host path, mounted at mountPath) is the
// working directory, so that local paths given to `aws s3 cp` resolve.
func AWSRunArgs(configFile, workdir, mountPath, user string, args []string, tty bool) []string {
	out := []string{"run", "--rm", "-i"}
	if tty {
		out = append(out, "-t")
	}
	out = append(out,
		"--network", "host",
		"--user", user,
		// The host user has no home in the image
		"-e", "HOME=/tmp",
		"-e", "AWS_CONFIG_FILE="+awsConfigMount,
		"-e", "AWS_PAGER=",
		"-v", configFile+":"+awsConfigMount+":ro",
		"-v", workdir+":"+mountPath,
		"-w", mountPath,
		AWSImage,
	)
	return append(out, args...)
}
