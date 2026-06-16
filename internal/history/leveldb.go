package history

import (
	"context"
	"errors"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

type LevelDBStore struct {
	db *leveldb.DB
}

func OpenLevelDB(path string) (*LevelDBStore, error) {
	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return nil, err
	}
	return &LevelDBStore{db: db}, nil
}

func (store *LevelDBStore) Close() error {
	return store.db.Close()
}

func (store *LevelDBStore) Put(ctx context.Context, key string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return store.db.Put([]byte(key), value, nil)
}

func (store *LevelDBStore) Get(ctx context.Context, key string) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	value, err := store.db.Get([]byte(key), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	return Entry{Key: key, Value: append([]byte(nil), value...)}, nil
}

func (store *LevelDBStore) GetPrefix(ctx context.Context, prefix string) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	iter := store.db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
	defer iter.Release()

	entries := []Entry{}
	for iter.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries = append(entries, Entry{
			Key:   string(iter.Key()),
			Value: append([]byte(nil), iter.Value()...),
		})
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	return entries, nil
}
