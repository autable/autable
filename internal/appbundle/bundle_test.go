package appbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"strings"
	"testing"
)

type entry struct {
	header tar.Header
	body   string
}

func file(name, body string) entry {
	return entry{header: tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}, body: body}
}

func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		header := entry.header
		if err := tw.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

const manifest = `{"database":"shop","name":"widgets","title":"Widgets","sdk":"1.0"}`

func validEntries() []entry {
	return []entry{
		file(ManifestName, manifest),
		{header: tar.Header{Name: "dist/", Mode: 0o755, Typeflag: tar.TypeDir}},
		file("dist/index.html", "<!doctype html><html><head></head></html>"),
		file("./dist/assets/x.js", "console.log(1)"),
		file("source/"+ManifestName, manifest),
		file("source/src/main.ts", "export {}"),
		file("source/package.json", "{}"),
	}
}

func TestParseValidBundle(t *testing.T) {
	bundle, err := Parse(archive(t, validEntries()...))
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manifest != (Manifest{Database: "shop", Name: "widgets", Title: "Widgets", SDK: "1.0"}) {
		t.Fatalf("unexpected manifest %#v", bundle.Manifest)
	}
	if len(bundle.Dist) != 2 || string(bundle.Dist["assets/x.js"]) != "console.log(1)" || bundle.Dist["index.html"] == nil {
		t.Fatalf("unexpected dist files %#v", bundle.Dist)
	}
}

func TestParseRejectsInvalidBundles(t *testing.T) {
	without := func(name string) []entry {
		var kept []entry
		for _, entry := range validEntries() {
			if entry.header.Name != name {
				kept = append(kept, entry)
			}
		}
		return kept
	}
	with := func(extra ...entry) []entry {
		return append(validEntries(), extra...)
	}
	cases := map[string]struct {
		entries []entry
		want    string
	}{
		"parent path":       {with(file("../x", "x")), "unsafe path"},
		"nested parent":     {with(file("dist/../../x", "x")), "unsafe path"},
		"absolute path":     {with(file("/etc/x", "x")), "unsafe path"},
		"backslash path":    {with(file(`dist\x.js`, "x")), "unsafe path"},
		"unclean path":      {with(file("dist//x.js", "x")), "unsafe path"},
		"symlink":           {with(entry{header: tar.Header{Name: "dist/link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}}), "not a regular file"},
		"hard link":         {with(entry{header: tar.Header{Name: "dist/link", Linkname: "dist/index.html", Typeflag: tar.TypeLink}}), "not a regular file"},
		"duplicate entry":   {with(file("dist/assets/x.js", "again")), "appears twice"},
		"missing index":     {without("dist/index.html"), "no dist/index.html"},
		"missing manifest":  {without(ManifestName), "no " + ManifestName},
		"unknown top-level": {with(file("node_modules/x.js", "x")), "unexpected bundle entry"},
		"manifest not json": {
			append(without(ManifestName), file(ManifestName, `{"database":`)),
			ManifestName,
		},
		"bad name": {
			append(without(ManifestName), file(ManifestName, `{"database":"shop","name":"Bad_Name","sdk":"1.0"}`)),
			"app name",
		},
		"missing database": {
			append(without(ManifestName), file(ManifestName, `{"name":"widgets","sdk":"1.0"}`)),
			"database is required",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(archive(t, tc.entries...))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestParseRejectsNonArchives(t *testing.T) {
	if _, err := Parse([]byte("plain text")); err == nil || !strings.Contains(err.Error(), "not gzip") {
		t.Fatalf("expected gzip error, got %v", err)
	}
	var gzipped bytes.Buffer
	gz := gzip.NewWriter(&gzipped)
	if _, err := gz.Write(bytes.Repeat([]byte("x"), 1024)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(gzipped.Bytes()); err == nil {
		t.Fatal("expected a gzip stream that is not a tar archive to fail")
	}
	if _, err := Parse(make([]byte, MaxCompressedBytes+1)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestParseEnforcesEntryLimit(t *testing.T) {
	entries := validEntries()
	for len(entries) <= MaxEntries {
		entries = append(entries, entry{header: tar.Header{Name: "source/dir/", Mode: 0o755, Typeflag: tar.TypeDir}})
	}
	if _, err := Parse(archive(t, entries...)); err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("expected entry limit error, got %v", err)
	}
}

func TestValidName(t *testing.T) {
	for _, name := range []string{"widgets", "a", "0-orders", "x" + strings.Repeat("y", 62)} {
		if err := ValidName(name); err != nil {
			t.Fatalf("expected %q valid, got %v", name, err)
		}
	}
	for _, name := range []string{"", "-widgets", "Widgets", "wid_gets", "wid.gets", "名称", "x" + strings.Repeat("y", 63)} {
		if err := ValidName(name); err == nil {
			t.Fatalf("expected %q invalid", name)
		}
	}
}

func TestSourceKeepsManifestAndSourceOnly(t *testing.T) {
	source, err := Source(archive(t, validEntries()...))
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gz)
	files := map[string]string{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := files[header.Name]; ok {
			t.Fatalf("duplicate source entry %q", header.Name)
		}
		files[header.Name] = string(body)
	}
	if len(files) != 3 || files[ManifestName] != manifest || files["src/main.ts"] != "export {}" || files["package.json"] != "{}" {
		t.Fatalf("unexpected source files %#v", files)
	}
	if _, err := Source([]byte("plain text")); err == nil {
		t.Fatal("expected invalid archive to fail")
	}
}
