// Package memorystorage provides an in-memory implementation of the storage interface,
// using JSON structures internally for compatibility with JSON-backed storage layers.
// It is suitable for temporary, non-persistent use cases such as testing or fast-access caching.
package memorystorage

import (
	"context"

	"github.com/patric-chuzhbe/urlshrt/internal/db/jsondb"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
)

// MemoryStorage is an in-memory implementation of a storage system for URL shortening.
// It embeds jsondb.JSONDB and initializes all required maps in memory, without reading
type MemoryStorage struct {
	*jsondb.JSONDB
}

// New creates and initializes a new instance of MemoryStorage with empty maps
// for all internal structures, making it ready for use immediately.
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

// Close is a no-op for MemoryStorage as there are no persistent resources to release.
// It is provided for interface compatibility.
func (theStorage *MemoryStorage) Close() error {
	return nil
}

// Ping is a stub method that always returns nil to signal the service is reachable.
// It is included to satisfy interfaces requiring a Ping method.
func (theStorage *MemoryStorage) Ping(ctx context.Context) error {
	return nil
}
