package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"autable/internal/history"
	"autable/internal/metadata"

	"github.com/syndtr/goleveldb/leveldb"
)

type copyUploader struct {
	destination string
	key         string
}

func (uploader *copyUploader) Upload(ctx context.Context, path string, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	uploader.key = key
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.Create(uploader.destination)
	if err != nil {
		return err
	}
	defer output.Close()
	_, err = io.Copy(output, input)
	return err
}

func TestRunCreatesArchiveAndUploads(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dataPath := filepath.Join(root, "data")
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		t.Fatal(err)
	}
	createSQLiteDatabase(t, filepath.Join(dataPath, "system.sqlite"), "settings", "system")
	createSQLiteDatabase(t, filepath.Join(dataPath, "workspace.sqlite"), "records", "workspace")
	historyStore, err := history.OpenLevelDB(filepath.Join(dataPath, "leveldb"))
	if err != nil {
		t.Fatal(err)
	}
	defer historyStore.Close()
	if err := historyStore.Put(ctx, "row/1", []byte("created")); err != nil {
		t.Fatal(err)
	}

	uploadedArchive := filepath.Join(root, "uploaded.tar.gz")
	uploader := &copyUploader{destination: uploadedArchive}
	result, err := Run(ctx, Options{
		DataPath:       dataPath,
		RepositoryPath: filepath.Join(root, "repository"),
		Catalog:        metadata.Catalog{Databases: []metadata.Database{{Name: "workspace"}}},
		IncludeLevelDB: true,
		TmpDir:         filepath.Join(root, "tmp"),
		ObjectPrefix:   "prod/autable",
		Now:            func() time.Time { return time.Date(2026, 6, 23, 7, 30, 0, 0, time.UTC) },
		Uploader:       uploader,
	}, historyStore)
	if err != nil {
		t.Fatal(err)
	}
	if result.Key != "prod/autable/autable-backup-20260623T073000Z.tar.gz" {
		t.Fatalf("unexpected result key: %q", result.Key)
	}
	if uploader.key != result.Key {
		t.Fatalf("uploader key mismatch: %q != %q", uploader.key, result.Key)
	}

	files := readArchive(t, uploadedArchive)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	requireArchiveFile(t, files, "manifest.json")
	requireArchiveFile(t, files, "sqlite/system.sqlite")
	requireArchiveFile(t, files, "sqlite/workspace.sqlite")
	requireArchivePrefix(t, names, "leveldb/")

	var manifest Manifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if !manifest.IncludeLevelDB || len(manifest.Files) != 3 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	assertLevelDBValue(t, files, filepath.Join(root, "restored-leveldb"), "row/1", "created")
	assertSQLiteValue(t, filepath.Join(root, "restored-system.sqlite"), files["sqlite/system.sqlite"], "settings", "system")
	assertSQLiteValue(t, filepath.Join(root, "restored-workspace.sqlite"), files["sqlite/workspace.sqlite"], "records", "workspace")
}

func createSQLiteDatabase(t *testing.T, path string, tableName string, value string) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE " + tableName + " (value TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO "+tableName+" (value) VALUES (?)", value); err != nil {
		t.Fatal(err)
	}
}

func readArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	files := map[string][]byte{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = data
	}
}

func requireArchiveFile(t *testing.T, files map[string][]byte, name string) {
	t.Helper()
	if _, ok := files[name]; !ok {
		t.Fatalf("expected archive file %q", name)
	}
}

func requireArchivePrefix(t *testing.T, names []string, prefix string) {
	t.Helper()
	for _, name := range names {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return
		}
	}
	t.Fatalf("expected archive entry with prefix %q, got %v", prefix, names)
}

func assertLevelDBValue(t *testing.T, files map[string][]byte, path string, key string, want string) {
	t.Helper()
	for name, data := range files {
		if len(name) < len("leveldb/") || name[:len("leveldb/")] != "leveldb/" {
			continue
		}
		destination := filepath.Join(path, filepath.FromSlash(name[len("leveldb/"):]))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.Get([]byte(key), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected leveldb value: got %q want %q", got, want)
	}
}

func assertSQLiteValue(t *testing.T, path string, data []byte, tableName string, want string) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	if err := db.QueryRow("SELECT value FROM " + tableName + " LIMIT 1").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("unexpected sqlite value from %s: got %q want %q", tableName, got, want)
	}
}
