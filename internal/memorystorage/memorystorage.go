package memorystorage

import (
	"context"
	"github.com/patric-chuzhbe/urlshrt/internal/simplejsondb"
)

type MemoryStorage struct {
	*simplejsondb.SimpleJSONDB
}

func New() (*MemoryStorage, error) {
	return &MemoryStorage{
		SimpleJSONDB: &simplejsondb.SimpleJSONDB{
			Cache: simplejsondb.CacheStruct{
				ShortToFull: map[string]string{},
				FullToShort: map[string]string{},
			},
		},
	}, nil
}

func (theStorage *MemoryStorage) Close() error {
	return nil
}

func (theStorage *MemoryStorage) Ping(outerCtx context.Context) error {
	return nil
}
