package grpcserver

import (
	"context"
	"errors"

	"github.com/go-playground/validator/v10"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	"github.com/patric-chuzhbe/urlshrt/internal/models"

	pb "github.com/patric-chuzhbe/urlshrt/internal/grpcserver/proto"
	"github.com/patric-chuzhbe/urlshrt/internal/service"
)

type ShortenerHandler struct {
	pb.UnimplementedShortenerServiceServer
	svc *service.Service
}

func NewShortenerHandler(svc *service.Service) *ShortenerHandler {
	return &ShortenerHandler{svc: svc}
}

func (h *ShortenerHandler) Shorten(ctx context.Context, req *pb.ShortenRequest) (*pb.ShortenResponse, error) {
	userID, ok := ctx.Value(auth.UserIDKey).(string)
	if !ok || userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user ID")
	}

	if req.GetUrl() == "" {
		return nil, status.Error(codes.InvalidArgument, "url must not be empty")
	}

	URLToShorten, err := h.svc.ExtractFirstURL(req.GetUrl())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	short, err := h.svc.ShortenURL(ctx, URLToShorten, userID)
	switch {
	case err == nil:
		return &pb.ShortenResponse{
			ShortUrl:      short,
			AlreadyExists: false,
		}, nil
	case errors.Is(err, service.ErrConflict):
		return &pb.ShortenResponse{
			ShortUrl:      short,
			AlreadyExists: true,
		}, nil
	default:
		return nil, status.Error(codes.Internal, "failed to shorten URL")
	}
}

func (h *ShortenerHandler) Resolve(ctx context.Context, req *pb.ResolveRequest) (*pb.ResolveResponse, error) {
	ShortURL := req.GetShortUrl()
	if ShortURL == "" {
		return nil, status.Error(codes.InvalidArgument, "short URL key must not be empty")
	}
	validatedShortURL, err := h.svc.ExtractFirstURL(ShortURL)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	shortKey := h.svc.GetShortURLKey(validatedShortURL)

	original, err := h.svc.GetOriginalURL(ctx, shortKey)

	switch {
	case errors.Is(err, service.ErrURLMarkedAsDeleted):
		return &pb.ResolveResponse{
			OriginalUrl: "",
			IsDeleted:   true,
			Found:       true,
		}, nil

	case err != nil:
		return nil, status.Error(codes.Internal, "failed to resolve URL")

	case original == "":
		return nil, status.Error(codes.NotFound, "short URL not found")

	default:
		return &pb.ResolveResponse{
			OriginalUrl: original,
			Found:       true,
			IsDeleted:   false,
		}, nil
	}
}

func (h *ShortenerHandler) Ping(ctx context.Context, _ *pb.PingRequest) (*pb.PingResponse, error) {
	if err := h.svc.Ping(ctx); err != nil {
		return nil, status.Error(codes.Unavailable, "storage is unavailable")
	}
	return &pb.PingResponse{Ok: true}, nil
}

func (h *ShortenerHandler) ShortenBatch(ctx context.Context, req *pb.ShortenBatchRequest) (*pb.ShortenBatchResponse, error) {
	userID, ok := ctx.Value(auth.UserIDKey).(string)
	if !ok || userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user ID")
	}

	if len(req.GetItems()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "batch items must not be empty")
	}

	batch := make(models.BatchShortenRequest, len(req.Items))
	for i, item := range req.Items {
		if item.GetCorrelationId() == "" || item.GetOriginalUrl() == "" {
			return nil, status.Error(codes.InvalidArgument, "each batch item must have correlation_id and original_url")
		}
		batch[i] = models.ShortenRequestItem{
			CorrelationID: item.CorrelationId,
			OriginalURL:   item.OriginalUrl,
		}
	}

	validate := validator.New()
	if err := validate.Var(batch, "dive"); err != nil {
		return nil, status.Error(codes.InvalidArgument, "found malforme dURL")
	}

	result, err := h.svc.BatchShortenURLs(ctx, batch, userID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to shorten batch URLs")
	}

	resp := &pb.ShortenBatchResponse{
		Results: make([]*pb.ShortenBatchResult, len(result)),
	}

	for i, r := range result {
		resp.Results[i] = &pb.ShortenBatchResult{
			CorrelationId: r.CorrelationID,
			ShortUrl:      r.ShortURL,
		}
	}

	return resp, nil
}

func (h *ShortenerHandler) GetUserURLs(ctx context.Context, req *pb.GetUserURLsRequest) (*pb.GetUserURLsResponse, error) {
	userID, ok := ctx.Value(auth.UserIDKey).(string)
	if !ok || userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user ID")
	}

	urls, err := h.svc.GetUserURLs(ctx, userID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to retrieve user URLs")
	}

	if len(urls) == 0 {
		return nil, status.Error(codes.NotFound, "no URLs found for user")
	}

	resp := &pb.GetUserURLsResponse{Urls: make([]*pb.UserURL, len(urls))}
	for i, u := range urls {
		resp.Urls[i] = &pb.UserURL{
			ShortUrl:    u.ShortURL,
			OriginalUrl: u.OriginalURL,
		}
	}

	return resp, nil
}

func (h *ShortenerHandler) DeleteUserURLs(ctx context.Context, req *pb.DeleteUserURLsRequest) (*pb.DeleteUserURLsResponse, error) {
	userID, ok := ctx.Value(auth.UserIDKey).(string)
	if !ok || userID == "" {
		return nil, status.Error(codes.Unauthenticated, "missing user ID")
	}

	if len(req.GetShortUrls()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "short_urls list must not be empty")
	}

	h.svc.DeleteURLsAsync(ctx, userID, req.GetShortUrls())
	return &pb.DeleteUserURLsResponse{Accepted: true}, nil
}

func (h *ShortenerHandler) GetInternalStats(ctx context.Context, _ *pb.GetInternalStatsRequest) (*pb.GetInternalStatsResponse, error) {
	stats, err := h.svc.GetInternalStats(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to retrieve internal stats")
	}

	return &pb.GetInternalStatsResponse{
		Urls:  stats.URLs,
		Users: stats.Users,
	}, nil
}
