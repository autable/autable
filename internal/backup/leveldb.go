package backup

import (
	"context"
	"os"
	"path/filepath"

	"autable/internal/history"

	"github.com/syndtr/goleveldb/leveldb"
)

func exportLevelDBSnapshot(ctx context.Context, store *history.LevelDBStore, destinationPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.RemoveAll(destinationPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return err
	}
	destinationDB, err := leveldb.OpenFile(destinationPath, nil)
	if err != nil {
		return err
	}
	closeDestination := true
	defer func() {
		if closeDestination {
			_ = destinationDB.Close()
		}
	}()

	err = store.ForEachSnapshot(ctx, func(key []byte, value []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return destinationDB.Put(key, value, nil)
	})
	if err != nil {
		return err
	}
	closeDestination = false
	return destinationDB.Close()
}
