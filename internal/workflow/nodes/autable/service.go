package autable

import (
	"context"

	"autable/internal/workflow"
)

type Service interface {
	CreateRow(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error)
	UpdateRow(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error)
	UpsertRow(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error)
	DeleteRow(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error)
	ListRows(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error)
	CreateFields(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error)
}
