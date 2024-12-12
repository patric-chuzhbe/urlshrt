package storage

import "context"

type Storage interface {
	Insert(outerCtx context.Context, short, full string) error
	Close() error
	FindFullByShort(outerCtx context.Context, short string) (string, bool, error)
	FindShortByFull(outerCtx context.Context, full string) (string, bool, error)
	IsShortExists(outerCtx context.Context, short string) (bool, error)
	Ping(outerCtx context.Context) error
}
