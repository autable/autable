package repository

import "path/filepath"

const (
	MetadataDir      = "metadata"
	MetadataFileName = "main.yml"
	WorkflowDir      = "workflow"
)

type Layout struct {
	root string
}

func NewLayout(root string) Layout {
	return Layout{root: root}
}

func (layout Layout) MetadataPath() string {
	return filepath.Join(layout.root, MetadataDir, MetadataFileName)
}

func (layout Layout) WorkflowDir() string {
	return filepath.Join(layout.root, WorkflowDir)
}
