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
				ShortToFull:       map[string]string{},
				FullToShort:       map[string]string{},
				Users:             map[int]*user.User{},
				NextUserID:        1,
				UsersIdsToUrlsMap: map[int][]string{},
				UrlsToUsersIdsMap: map[string][]int{},
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
