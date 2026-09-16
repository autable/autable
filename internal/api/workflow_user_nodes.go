package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"autable/internal/workflow"
)

func (service workflowAutableService) GetUser(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	if info.CreatorID == "" {
		return nil, errors.New("workflow creator is required")
	}
	userID, _ := input["user_id"].(string)
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("user_id is required")
	}
	user, err := service.server.system.User(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("user %q not found", userID)
		}
		return nil, err
	}
	return map[string]any{
		"id":            user.ID,
		"email":         user.Email,
		"display_name":  user.DisplayName,
		"provider":      string(user.Provider),
		"provider_name": user.ProviderName,
	}, nil
}
