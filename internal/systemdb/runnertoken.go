package systemdb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ResetRunnerToken generates a new runner token for the database, stores its
// SHA-256 hash, and returns the plaintext. The plaintext is never stored and
// cannot be retrieved again; resetting replaces the database's previous
// token. Runners are database-scoped: the token a runner presents decides
// which database it serves.
func (db *DB) ResetRunnerToken(ctx context.Context, dbName string) (string, error) {
	if dbName == "" {
		return "", errors.New("database name is required")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := "atr_" + base64.RawURLEncoding.EncodeToString(raw)
	model := runnerTokenModel{
		DatabaseName: dbName,
		TokenHash:    hashRunnerToken(token),
		CreatedAt:    time.Now().UTC().UnixMilli(),
	}
	err := db.orm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "database_name"}},
		UpdateAll: true,
	}).Create(&model).Error
	if err != nil {
		return "", err
	}
	return token, nil
}

// LookupRunnerToken resolves a presented token to the database it belongs
// to; ok is false when the token matches no database.
func (db *DB) LookupRunnerToken(ctx context.Context, token string) (string, bool, error) {
	var model runnerTokenModel
	err := db.orm.WithContext(ctx).
		Where(&runnerTokenModel{TokenHash: hashRunnerToken(token)}).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return model.DatabaseName, true, nil
}

// RunnerTokenCreatedAt returns the creation timestamp of the database's
// runner token; the second result is false when none has been generated.
func (db *DB) RunnerTokenCreatedAt(ctx context.Context, dbName string) (int64, bool, error) {
	var model runnerTokenModel
	err := db.orm.WithContext(ctx).
		Where(&runnerTokenModel{DatabaseName: dbName}).
		First(&model).Error
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
