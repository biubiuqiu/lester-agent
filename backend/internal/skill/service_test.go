package skill

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestZipBundleRoundTrip(t *testing.T) {
	bundle, err := zipBundle(map[string][]byte{"SKILL.md": []byte("# Test"), "references/example.md": []byte("example")})
	if err != nil {
		t.Fatal(err)
	}
	files, err := unzipBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if string(files["SKILL.md"]) != "# Test" || string(files["references/example.md"]) != "example" {
		t.Fatalf("unexpected extracted files: %#v", files)
	}
}

func TestUnzipBundleRejectsTraversal(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	entry, err := archive.Create("../SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("unsafe"))
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = unzipBundle(buffer.Bytes()); err == nil {
		t.Fatal("path traversal entry was accepted")
	}
}
