package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autable/internal/appsdk"
	"autable/internal/metadata"
	"autable/internal/repositorysync"
)

type recordingSync struct{ changes []repositorysync.Change }

func (sync *recordingSync) Notify(change repositorysync.Change) {
	sync.changes = append(sync.changes, change)
}

func TestAppVersionsAreMirroredIntoTheRepository(t *testing.T) {
	catalog := metadata.Catalog{Databases: []metadata.Database{{Name: "名称", Tables: []metadata.Table{}}}}
	server, system, _ := newTestServerWithMetadataFile(t, catalog)
	makeTestAdmins(server, "builder")
	root := t.TempDir()
	recorder := &recordingSync{}
	server.SetRepositorySync(recorder)
	server.SetRepositoryRoot(root)
	dir := filepath.Join(root, "app", "名称", "widgets")

	createTestApp(t, server, system, "builder", "名称", "widgets")
	readVersion := func() mirroredVersion {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, versionFileName))
		if err != nil {
			t.Fatal(err)
		}
		var version mirroredVersion
		if err := json.Unmarshal(data, &version); err != nil {
			t.Fatal(err)
		}
		return version
	}
	source, err := os.ReadFile(filepath.Join(dir, "src", "main.ts"))
	if err != nil || !strings.Contains(string(source), "version-1") {
		t.Fatalf("expected v1 source in the repository, got %q, %v", source, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist")); !os.IsNotExist(err) {
		t.Fatalf("expected no build output in the repository, got %v", err)
	}
	if version := readVersion(); version.Version != 1 || version.PublishedVersion != 1 || version.SDK != appsdk.Version() {
		t.Fatalf("unexpected version file: %#v", version)
	}

	second := uploadTestApp(t, server, system, "builder", "名称", "widgets", testAppBundle(t, "名称", "widgets", appsdk.Version(), "version-2"), "message=second")
	if second.Code != http.StatusOK {
		t.Fatalf("expected second upload 200, got %d: %s", second.Code, second.Body.String())
	}
	source, _ = os.ReadFile(filepath.Join(dir, "src", "main.ts"))
	if !strings.Contains(string(source), "version-2") {
		t.Fatalf("expected v2 source to replace v1, got %q", source)
	}
	if version := readVersion(); version.Version != 2 || version.PublishedVersion != 1 || version.Message != "second" {
		t.Fatalf("unexpected version file after second push: %#v", version)
	}

	if publish := publishTestApp(t, server, system, "builder", "名称", "widgets", 2); publish.Code != http.StatusOK {
		t.Fatalf("expected publish 200, got %d", publish.Code)
	}
	if version := readVersion(); version.PublishedVersion != 2 {
		t.Fatalf("expected published version 2 in the repository, got %#v", version)
	}

	deleted := appTestRequest(t, server, system, "builder", http.MethodDelete, "/api/databases/%E5%90%8D%E7%A7%B0/apps/widgets", nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("expected delete 204, got %d", deleted.Code)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected mirrored source to be removed, got %v", err)
	}

	actions := []string{}
	for _, change := range recorder.changes {
		actions = append(actions, change.Action)
		if len(change.Paths) != 1 || change.Paths[0] != "app/名称/widgets" {
			t.Fatalf("unexpected change paths: %#v", change)
		}
	}
	if strings.Join(actions, ",") != "app.push,app.push,app.publish,app.delete" {
		t.Fatalf("unexpected repository changes: %v", actions)
	}
}
