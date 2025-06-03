package memorystorage

import (
	"context"

	"github.com/patric-chuzhbe/urlshrt/internal/db/jsondb"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
)

type MemoryStorage struct {
	*jsondb.JSONDB
}

func New() (*MemoryStorage, error) {
	return &MemoryStorage{
		JSONDB: &jsondb.JSONDB{
			Cache: jsondb.CacheStruct{
				ShortToFull:        map[string]string{},
				FullToShort:        map[string]string{},
				Users:              map[string]*user.User{},
				UsersIdsToUrlsMap:  map[string][]string{},
				UrlsToUsersIdsMap:  map[string][]string{},
				UrlsToIsDeletedMap: map[string]bool{},
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
