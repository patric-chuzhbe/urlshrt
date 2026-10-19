package grpcserver

import (
	"context"
	"database/sql"
	"net"

	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	"github.com/patric-chuzhbe/urlshrt/internal/user"

	"github.com/patric-chuzhbe/urlshrt/internal/grpcserver/interceptor"

	"google.golang.org/grpc"

	pb "github.com/patric-chuzhbe/urlshrt/internal/grpcserver/proto"
)

type authenticator interface {
	GetUserIDFromToken(tokenString string) (string, error)
	BuildJWTString(claims *auth.Claims) (string, error)
}

type userKeeper interface {
	CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (string, error)
	GetUserByID(ctx context.Context, userID string, transaction *sql.Tx) (*user.User, error)
}

func NewGRPCServer(
	addr string,
	handler *ShortenerHandler,
	auth authenticator,
	db userKeeper,
) (*grpc.Server, net.Listener, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	authInterceptor := interceptor.NewAuthInterceptor(auth, db)

	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptor.UnaryLoggingInterceptor([]string{
				"/shortener.ShortenerService/Shorten",
				"/shortener.ShortenerService/Resolve",
				"/shortener.ShortenerService/Ping",
				"/shortener.ShortenerService/ShortenBatch",
				"/shortener.ShortenerService/GetUserURLs",
				"/shortener.ShortenerService/DeleteUserURLs",
				"/shortener.ShortenerService/GetInternalStats",
			}),
			authInterceptor.UnaryAuthInterceptor([]string{
				"/shortener.ShortenerService/Shorten",
				"/shortener.ShortenerService/ShortenBatch",
				"/shortener.ShortenerService/GetUserURLs",
				"/shortener.ShortenerService/DeleteUserURLs",
			}),
			authInterceptor.UnaryRegisterNewUserInterceptor([]string{
				"/shortener.ShortenerService/Shorten",
				"/shortener.ShortenerService/ShortenBatch",
				"/shortener.ShortenerService/GetUserURLs",
			}),
		),
	)
	pb.RegisterShortenerServiceServer(server, handler)

	return server, lis, nil
}
