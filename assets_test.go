package assets

import (
	"bytes"
	"io/fs"
	"os"
	"slices"
	"testing"
)

// Every listed resource is embedded, byte for byte as in the repository.
func TestFilesAreEmbedded(t *testing.T) {
	for _, name := range Files {
		embedded, err := FS.ReadFile(name)
		if err != nil {
			t.Errorf("%s: not embedded: %v", name, err)
			continue
		}
		onDisk, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(embedded, onDisk) {
			t.Errorf("%s: embedded content differs from the repository", name)
		}
	}
}

// Nothing is embedded without being listed, so Files stays the reference.
func TestNoUnlistedFile(t *testing.T) {
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !slices.Contains(Files, path) {
			t.Errorf("%s is embedded but missing from Files", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStaticFilesAreEmbedded(t *testing.T) {
	for _, name := range StaticFiles {
		if !slices.Contains(Files, name) {
			t.Errorf("static file %s is not in Files", name)
		}
	}
}
