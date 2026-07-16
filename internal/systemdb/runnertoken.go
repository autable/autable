package systemdb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const runnerTokenRowID = 1

// ResetRunnerToken generates a new runner token, stores its SHA-256 hash, and
// returns the plaintext. The plaintext is never stored and cannot be
// retrieved again; resetting replaces the previous token.
func (db *DB) ResetRunnerToken(ctx context.Context) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := "atr_" + base64.RawURLEncoding.EncodeToString(raw)
	model := runnerTokenModel{
		ID:        runnerTokenRowID,
		TokenHash: hashRunnerToken(token),
		CreatedAt: time.Now().UTC().UnixMilli(),
	}
	err := db.orm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(&model).Error
	if err != nil {
		return "", err
	}
	return token, nil
}

// ValidateRunnerToken reports whether token matches the stored runner token.
// It is false when no token has been generated yet.
func (db *DB) ValidateRunnerToken(ctx context.Context, token string) (bool, error) {
	var model runnerTokenModel
	err := db.orm.WithContext(ctx).First(&model, runnerTokenRowID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	match := subtle.ConstantTimeCompare([]byte(model.TokenHash), []byte(hashRunnerToken(token)))
	return match == 1, nil
}

// RunnerTokenCreatedAt returns the creation timestamp of the current runner
// token; the second result is false when no token has been generated yet.
func (db *DB) RunnerTokenCreatedAt(ctx context.Context) (int64, bool, error) {
	var model runnerTokenModel
	err := db.orm.WithContext(ctx).First(&model, runnerTokenRowID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return model.CreatedAt, true, nil
}

func hashRunnerToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
