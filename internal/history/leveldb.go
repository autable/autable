package history

import (
	"context"
	"errors"
	"slices"

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

func (store *LevelDBStore) GetPrefixLimit(ctx context.Context, prefix string, limit int) ([]Entry, error) {
	if limit <= 0 {
		return []Entry{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	iter := store.db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
	defer iter.Release()

	entries := []Entry{}
	for ok := iter.Last(); ok && len(entries) < limit; ok = iter.Prev() {
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
	slices.Reverse(entries)
	return entries, nil
}

func (store *LevelDBStore) GetPrefixKeysLimit(ctx context.Context, prefix string, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	iter := store.db.NewIterator(util.BytesPrefix([]byte(prefix)), nil)
	defer iter.Release()

	keys := []string{}
	for ok := iter.Last(); ok && len(keys) < limit; ok = iter.Prev() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		keys = append(keys, string(iter.Key()))
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	slices.Reverse(keys)
	return keys, nil
}

func (store *LevelDBStore) ForEachSnapshot(ctx context.Context, visit func(key []byte, value []byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot, err := store.db.GetSnapshot()
	if err != nil {
		return err
	}
	defer snapshot.Release()

	iter := snapshot.NewIterator(nil, nil)
	defer iter.Release()

	for iter.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := visit(append([]byte(nil), iter.Key()...), append([]byte(nil), iter.Value()...)); err != nil {
			return err
		}
	}
	return iter.Error()
}
