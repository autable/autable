package systemdb

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"gorm.io/gorm"
)

func TestLibraryVersionsPublishAndImporters(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "system.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	text, err := db.CreateLibrary(ctx, Library{Name: "text", Title: "Text", CreatorID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateLibrary(ctx, Library{Name: "text"}); err == nil {
		t.Fatal("expected duplicate library names to be rejected")
	}
	if _, err := db.LibraryByName(ctx, "missing"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	text, v1, err := db.AddLibraryVersion(ctx, text.ID, "Text v1", LibraryVersion{SDK: "1.0", Message: "first", CreatedBy: "u1"}, []byte("bundle-1"))
	if err != nil || v1.Version != 1 || v1.Size != 8 || text.LatestVersion != 1 || text.PublishedVersion != 0 || text.Title != "Text v1" {
		t.Fatalf("unexpected first version %#v / %#v, %v", v1, text, err)
	}
	if _, _, err := db.AddLibraryVersion(ctx, text.ID, "", LibraryVersion{SDK: "1.0"}, []byte("bundle-2")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublishLibraryVersion(ctx, text.ID, 9, nil); !errors.Is(err, ErrLibraryVersionNotFound) {
		t.Fatalf("expected a missing version error, got %v", err)
	}
	published, err := db.PublishLibraryVersion(ctx, text.ID, 2, nil)
	if err != nil || published.PublishedVersion != 2 {
		t.Fatalf("unexpected publish %#v, %v", published, err)
	}
	if bundle, err := db.LibraryVersionBundle(ctx, text.ID, 1); err != nil || string(bundle) != "bundle-1" {
		t.Fatalf("unexpected bundle %q, %v", bundle, err)
	}
	if versions, err := db.LibraryVersions(ctx, text.ID); err != nil || len(versions) != 2 || versions[0].Version != 2 {
		t.Fatalf("expected versions newest first, got %#v, %v", versions, err)
	}

	// A library importing text, and a workflow importing both.
	format, _ := db.CreateLibrary(ctx, Library{Name: "format"})
	_, _, _ = db.AddLibraryVersion(ctx, format.ID, "", LibraryVersion{SDK: "1.0"}, []byte("f"))
	if _, err := db.PublishLibraryVersion(ctx, format.ID, 1, []string{"text"}); err != nil {
		t.Fatal(err)
	}
	if version, _ := db.LibraryVersion(ctx, format.ID, 1); !slices.Equal(version.Imports, []string{"text"}) {
		t.Fatalf("expected recorded imports, got %#v", version)
	}
	workflow, _ := db.CreateWorkflow(ctx, Workflow{DatabaseName: "db", Name: "sync", Title: "Sync"})
	_, _, _ = db.AddWorkflowVersion(ctx, workflow.ID, "", WorkflowVersion{SDK: "1.0"}, []byte("w1"))
	_, _, _ = db.AddWorkflowVersion(ctx, workflow.ID, "", WorkflowVersion{SDK: "1.0"}, []byte("w2"))
	if _, err := db.PublishWorkflowVersion(ctx, workflow.ID, 1, "[]", []string{"text", "format"}); err != nil {
		t.Fatal(err)
	}
	// "texts" must not match "text".
	other, _ := db.CreateLibrary(ctx, Library{Name: "texts"})
	_, _, _ = db.AddLibraryVersion(ctx, other.ID, "", LibraryVersion{SDK: "1.0"}, []byte("t"))
	if _, err := db.PublishLibraryVersion(ctx, other.ID, 1, nil); err != nil {
		t.Fatal(err)
	}

	importers, err := db.LibraryImporters(ctx, "text")
	if err != nil {
		t.Fatal(err)
	}
	want := []LibraryImporter{
		{Kind: "workflow", DatabaseName: "db", Name: "sync", Title: "Sync", Version: 1},
		{Kind: "library", Name: "format", Version: 1},
	}
	if !slices.Equal(importers, want) {
		t.Fatalf("unexpected importers %#v", importers)
	}
	if importers, _ := db.LibraryImporters(ctx, "texts"); len(importers) != 0 {
		t.Fatalf("expected no importers of texts, got %#v", importers)
	}
	// Publishing a version without the import drops the workflow.
	if _, err := db.PublishWorkflowVersion(ctx, workflow.ID, 2, "[]", nil); err != nil {
		t.Fatal(err)
	}
	if importers, _ := db.LibraryImporters(ctx, "text"); len(importers) != 1 || importers[0].Kind != "library" {
		t.Fatalf("expected only the library importer, got %#v", importers)
	}
	imports, err := db.PublishedLibraryImports(ctx)
	if err != nil || !slices.Equal(imports["format"], []string{"text"}) || len(imports["text"]) != 0 || len(imports) != 3 {
		t.Fatalf("unexpected published imports %#v, %v", imports, err)
	}

	if err := db.DeleteLibrary(ctx, text.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LibraryByName(ctx, "text"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected the library to be gone, got %v", err)
	}
	if _, err := db.LibraryVersionBundle(ctx, text.ID, 1); !errors.Is(err, ErrLibraryVersionNotFound) {
		t.Fatalf("expected its versions to be gone, got %v", err)
	}
}
