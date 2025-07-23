package interceptor

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/patric-chuzhbe/urlshrt/internal/logger"
)

// UnaryLoggingInterceptor logs each incoming unary gRPC request with method and duration.
func UnaryLoggingInterceptor(allowedMethods []string) grpc.UnaryServerInterceptor {
	allowed := make(map[string]struct{}, len(allowedMethods))
	for _, m := range allowedMethods {
		allowed[m] = struct{}{}
	}

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		if _, ok := allowed[info.FullMethod]; !ok {
			return handler(ctx, req)
		}

		start := time.Now()

		resp, err = handler(ctx, req)

		duration := time.Since(start)
		st, _ := status.FromError(err)

		logger.Log.Infoln(
			"gRPC request",
			"method", info.FullMethod,
			"duration", duration,
			"code", st.Code().String(),
			"message", st.Message(),
		)

		return resp, err
	}
}
