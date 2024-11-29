package storage

import "context"

type Storage interface {
	Insert(short, full string)
	Close() error
	FindFullByShort(short string) (full string, found bool)
	FindShortByFull(full string) (short string, found bool)
	IsShortExists(short string) bool
	Ping(outerCtx context.Context) error
}
