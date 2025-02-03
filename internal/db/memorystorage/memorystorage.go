package memorystorage

import (
	"context"
	"github.com/patric-chuzhbe/urlshrt/internal/db/jsondb"
)

type MemoryStorage struct {
	*jsondb.JSONDB
}

func New() (*MemoryStorage, error) {
	return &MemoryStorage{
		JSONDB: &jsondb.JSONDB{
			Cache: jsondb.CacheStruct{
				ShortToFull: map[string]string{},
				FullToShort: map[string]string{},
			},
		},
	}, nil
}

func (theStorage *MemoryStorage) Close() error {
	return nil
}

func (theStorage *MemoryStorage) Ping(ctx context.Context) error {
	return nil
}
