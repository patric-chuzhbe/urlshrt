package interceptor

import (
	"context"
	"database/sql"
	"errors"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
)

type authenticator interface {
	GetUserIDFromToken(tokenString string) (string, error)
	BuildJWTString(claims *auth.Claims) (string, error)
}

type userKeeper interface {
	CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (string, error)
	GetUserByID(ctx context.Context, userID string, transaction *sql.Tx) (*user.User, error)
}

type AuthInterceptor struct {
	auth authenticator
	db   userKeeper
}

func NewAuthInterceptor(auth authenticator, db userKeeper) *AuthInterceptor {
	return &AuthInterceptor{auth: auth, db: db}
}

// UnaryAuthInterceptor extracts user ID from authorization metadata and attaches it to the context.
func (a *AuthInterceptor) UnaryAuthInterceptor(allowedMethods []string) grpc.UnaryServerInterceptor {
	allowed := make(map[string]struct{}, len(allowedMethods))
	for _, m := range allowedMethods {
		allowed[m] = struct{}{}
	}

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if _, ok := allowed[info.FullMethod]; !ok {
			return handler(ctx, req)
		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return handler(ctx, req)
		}

		authHeader := md.Get("authorization")
		if len(authHeader) == 0 {
			return handler(ctx, req)
		}

		userID, err := a.auth.GetUserIDFromToken(authHeader[0])
		if err != nil && !errors.Is(err, auth.ErrInvalidTokenOrJwtParsing) {
			logger.Log.Debugln("Error calling the `a.auth.GetUserIDFromToken()`: ", zap.Error(err))
			return nil, status.Errorf(codes.Internal, "Error calling the `a.auth.GetUserIDFromToken()`: %v", err)
		}
		if errors.Is(err, auth.ErrInvalidTokenOrJwtParsing) {
			logger.Log.Debugln("Error calling the `a.auth.GetUserIDFromToken()`: ", zap.Error(err))
		}

		usr, err := a.db.GetUserByID(ctx, userID, nil)
		if err != nil {
			logger.Log.Debugln("Error calling the `a.db.GetUserByID()`: ", zap.Error(err))
			return nil, status.Errorf(codes.Internal, "Error calling the `a.db.GetUserByID()`: %v", err)
		}

		ctxWithUser := context.WithValue(ctx, auth.UserIDKey, usr.ID)
		return handler(ctxWithUser, req)
	}
}

func (a *AuthInterceptor) UnaryRegisterNewUserInterceptor(allowedMethods []string) grpc.UnaryServerInterceptor {
	allowed := make(map[string]struct{}, len(allowedMethods))
	for _, m := range allowedMethods {
		allowed[m] = struct{}{}
	}

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if _, ok := allowed[info.FullMethod]; !ok {
			return handler(ctx, req)
		}

		var userID string

		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			if authHeader := md.Get("authorization"); len(authHeader) > 0 && authHeader[0] != "" {
				decodedUserID, err := a.auth.GetUserIDFromToken(authHeader[0])
				if err == nil {
					userID = decodedUserID
				} else {
					logger.Log.Debug("invalid JWT token in metadata", zap.String("token", authHeader[0]), zap.Error(err))
				}
			}
		}

		if userID == "" {
			var err error
			userID, err = a.db.CreateUser(ctx, &user.User{}, nil)
			if err != nil {
				logger.Log.Error("failed to create user", zap.Error(err))
				return nil, status.Errorf(codes.Internal, "could not register new user")
			}

			token, err := a.auth.BuildJWTString(&auth.Claims{UserID: userID})
			if err != nil {
				logger.Log.Error("failed to generate JWT", zap.Error(err))
				return nil, status.Errorf(codes.Internal, "could not generate token")
			}

			if sendErr := grpc.SendHeader(ctx, metadata.Pairs("authorization", token)); sendErr != nil {
				logger.Log.Warn("failed to send authorization header", zap.Error(sendErr))
			}
		}

		ctxWithUser := context.WithValue(ctx, auth.UserIDKey, userID)
		return handler(ctxWithUser, req)
	}
}
