package grpcserver

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"regexp"
	"time"

	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	"github.com/patric-chuzhbe/urlshrt/internal/config"
	"github.com/patric-chuzhbe/urlshrt/internal/db/memorystorage"
	"github.com/patric-chuzhbe/urlshrt/internal/db/postgresdb"
	pb "github.com/patric-chuzhbe/urlshrt/internal/grpcserver/proto"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/mockstorage"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/service"
	"github.com/patric-chuzhbe/urlshrt/internal/user"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type testStorage interface {
	BeginTransaction() (*sql.Tx, error)
	FindShortByFull(ctx context.Context, full string, tx *sql.Tx) (string, bool, error)
	InsertURLMapping(ctx context.Context, short, full string, tx *sql.Tx) error
	SaveUserUrls(ctx context.Context, userID string, urls []string, tx *sql.Tx) error
	CommitTransaction(tx *sql.Tx) error
	RollbackTransaction(tx *sql.Tx) error
	FindFullByShort(ctx context.Context, short string) (string, bool, error)
	Ping(ctx context.Context) error
	FindShortsByFulls(
		ctx context.Context,
		originalUrls []string,
		transaction *sql.Tx,
	) (map[string]string, error)
	SaveNewFullsAndShorts(
		ctx context.Context,
		unexistentFullsToShortsMap map[string]string,
		transaction *sql.Tx,
	) error
	GetUserUrls(
		ctx context.Context,
		userID string,
		shortURLFormatter models.URLFormatter,
	) (models.UserUrls, error)
	GetNumberOfShortenedURLs(ctx context.Context) (int64, error)
	GetNumberOfUsers(ctx context.Context) (int64, error)
	CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (string, error)
	GetUserByID(ctx context.Context, userID string, transaction *sql.Tx) (*user.User, error)
}

type mockUrlsRemover struct {
	jobs []*models.URLDeleteJob
}

type initOptions struct {
	mockAuth    bool
	mockStorage testStorage
}

type initOption func(*initOptions)

const (
	addr          = "localhost:0"
	dialTimeout   = 5 * time.Second
	databaseDSN   = "" // host=localhost user=diplomauser password=Ga6V0W0Ukn2s2tFn3uku7AAp2GAoy5 dbname=diploma sslmode=disable
	migrationsDir = "../../migrations"
)

type mockAuth struct{}

func (a *mockAuth) GetUserIDFromToken(tokenString string) (string, error) {
	return "user-id", nil
}

func (a *mockAuth) BuildJWTString(claims *auth.Claims) (string, error) {
	return "user-id-jwt", nil
}

func withMockAuth(value bool) initOption {
	return func(options *initOptions) {
		options.mockAuth = value
	}
}

func withMockStorage(db testStorage) initOption {
	return func(options *initOptions) {
		options.mockStorage = db
	}
}

func (m *mockUrlsRemover) EnqueueJob(job *models.URLDeleteJob) {
	m.jobs = append(m.jobs, job)
}

// startTestGRPCServer boots up a test gRPC server and returns the client and shutdown function.
func startTestGRPCServer(t *testing.T, optionsProto ...initOption) (pb.ShortenerServiceClient, func(), testStorage, authenticator) {
	options := &initOptions{}
	for _, protoOption := range optionsProto {
		protoOption(options)
	}

	err := logger.Init("debug")
	if t != nil {
		require.NoError(t, err)
	}

	cfg, err := config.New(config.WithDisableFlagsParsing(true))
	if t != nil {
		require.NoError(t, err)
	}

	var db testStorage
	if options.mockStorage != nil {
		db = options.mockStorage
	} else if databaseDSN != "" {
		db, err = postgresdb.New(
			context.Background(),
			databaseDSN,
			cfg.DBConnectionTimeout,
			migrationsDir,
			postgresdb.WithDBPreReset(true),
		)
	} else {
		db, err = memorystorage.New()
	}
	require.NoError(t, err)

	urlsRemover := &mockUrlsRemover{}

	s := service.New(
		db,
		urlsRemover,
		cfg.ShortURLBase,
	)

	authCookieSigningSecretKey, err := base64.URLEncoding.DecodeString(cfg.AuthCookieSigningSecretKey)
	require.NoError(t, err)

	var authInterceptor authenticator

	if options.mockAuth {
		authInterceptor = &mockAuth{}
	} else {
		authInterceptor = auth.New(
			db,
			cfg.AuthCookieName,
			authCookieSigningSecretKey,
		)
	}

	server, lis, err := NewGRPCServer(
		addr,
		NewShortenerHandler(s),
		authInterceptor,
		db,
	)
	require.NoError(t, err)

	go func() {
		if err := server.Serve(lis); err != nil {
			t.Logf("gRPC server stopped: %v", err)
		}
	}()

	dialContext, cancelDial := context.WithTimeout(context.Background(), dialTimeout)
	defer cancelDial()

	conn, err := grpc.DialContext(
		dialContext,
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(t, err)

	client := pb.NewShortenerServiceClient(conn)
	return client,
		func() {
			server.Stop()
			conn.Close()
			lis.Close()
		},
		db,
		authInterceptor
}

func TestShorten_Success(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()
	req := &pb.ShortenRequest{Url: "https://example.com"}

	resp, err := client.Shorten(ctx, req)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.ShortUrl)

	resolveResponse, err := client.Resolve(
		ctx,
		&pb.ResolveRequest{
			ShortUrl: resp.ShortUrl,
		},
	)
	assert.NoError(t, err)
	assert.Equal(
		t,
		"https://example.com",
		resolveResponse.OriginalUrl,
		"Resolved URL didn't match the cource URL",
	)
}

func TestShorten_EmptyURL(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()
	req := &pb.ShortenRequest{Url: ""}

	_, err := client.Shorten(ctx, req)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestShorten_InvalidURL(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()
	req := &pb.ShortenRequest{Url: "ht!tp:// bad_url"}

	_, err := client.Shorten(ctx, req)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestShorten_DuplicateURL(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()
	url := "https://repeat.com"

	firstResp, err := client.Shorten(ctx, &pb.ShortenRequest{Url: url})
	assert.NoError(t, err)
	assert.False(t, firstResp.AlreadyExists)

	secondResp, err := client.Shorten(ctx, &pb.ShortenRequest{Url: url})
	assert.NoError(t, err)
	assert.Equal(t, firstResp.ShortUrl, secondResp.ShortUrl, "Should return same shortened URL for duplicates")
	assert.True(t, secondResp.AlreadyExists)
}

func TestShorten_InternalError(t *testing.T) {
	db := new(mockstorage.StorageMock)
	client, shutdown, _, _ := startTestGRPCServer(t, withMockStorage(db))
	defer shutdown()

	db.On(
		"FindShortByFull",
		mock.Anything,
		mock.Anything,
		mock.Anything,
	).
		Return(
			"",
			false,
			errors.New("db error"),
		)

	db.On(
		"CreateUser",
		mock.Anything,
		mock.Anything,
		mock.Anything,
	).Return("user-id", nil)

	db.On("BeginTransaction").Return(nil, nil)

	db.On("RollbackTransaction", mock.Anything).Return(error(nil))

	ctx := context.Background()
	req := &pb.ShortenRequest{Url: "https://valid.com"}

	_, err := client.Shorten(ctx, req)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestResolve_NotFound(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()
	_, err := client.Resolve(ctx, &pb.ResolveRequest{
		ShortUrl: "http://nonexistent123.com",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestResolve_EmptyShortURL(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()
	_, err := client.Resolve(ctx, &pb.ResolveRequest{
		ShortUrl: "",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestResolve_MalformedShortURL(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()
	_, err := client.Resolve(ctx, &pb.ResolveRequest{
		ShortUrl: "ht!p://!!bad@@@",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestPing_Success(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()
	resp, err := client.Ping(ctx, &pb.PingRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Ok, "Expected Ping to return ok=true")
}

func TestPing_DBFailure(t *testing.T) {
	db := new(mockstorage.StorageMock)
	client, shutdown, _, _ := startTestGRPCServer(t, withMockStorage(db))
	defer shutdown()

	db.On(
		"Ping",
		mock.Anything,
	).Return(errors.New("db error"))

	db.On(
		"CreateUser",
		mock.Anything,
		mock.Anything,
		mock.Anything,
	).Return("user-id", nil)

	db.On("BeginTransaction").Return(nil, nil)

	db.On("RollbackTransaction", mock.Anything).Return(error(nil))

	ctx := context.Background()
	_, err := client.Ping(ctx, &pb.PingRequest{})
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code(), "Expected Ping to return UNAVAILABLE on failure")
}

func TestShortenBatch_Success(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()

	req := &pb.ShortenBatchRequest{
		Items: []*pb.ShortenBatchItem{
			{CorrelationId: "1", OriginalUrl: "https://a.com"},
			{CorrelationId: "2", OriginalUrl: "https://b.com"},
			{CorrelationId: "3", OriginalUrl: "https://c.com"},
		},
	}

	resp, err := client.ShortenBatch(ctx, req)
	require.NoError(t, err)
	require.Len(t, resp.Results, 3)

	resultMap := make(map[string]string)
	for _, r := range resp.Results {
		require.NotEmpty(t, r.ShortUrl)
		resultMap[r.CorrelationId] = r.ShortUrl
	}

	assert.Contains(t, resultMap, "1")
	assert.Contains(t, resultMap, "2")
	assert.Contains(t, resultMap, "3")
}

func TestShortenBatch_DuplicateURLs(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()

	req := &pb.ShortenBatchRequest{
		Items: []*pb.ShortenBatchItem{
			{CorrelationId: "a", OriginalUrl: "https://same.com"},
			{CorrelationId: "b", OriginalUrl: "https://same.com"},
		},
	}

	resp, err := client.ShortenBatch(ctx, req)
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)

	resolveResponse, err := client.Resolve(
		ctx,
		&pb.ResolveRequest{
			ShortUrl: resp.Results[0].ShortUrl,
		},
	)
	assert.NoError(t, err)
	assert.Equal(
		t,
		"https://same.com",
		resolveResponse.OriginalUrl,
		"Resolved URL didn't match the cource URL",
	)
}

func TestShortenBatch_EmptyRequest(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()

	_, err := client.ShortenBatch(ctx, &pb.ShortenBatchRequest{})
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestShortenBatch_MalformedURL(t *testing.T) {
	client, shutdown, _, _ := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()

	req := &pb.ShortenBatchRequest{
		Items: []*pb.ShortenBatchItem{
			{CorrelationId: "x", OriginalUrl: "http://valid.com"},
			{CorrelationId: "y", OriginalUrl: "!!bad!!url"},
		},
	}

	_, err := client.ShortenBatch(ctx, req)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetUserURLs_Success(t *testing.T) {
	client, shutdown, db, authenticator := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()

	userID, err := db.CreateUser(ctx, &user.User{}, nil)
	require.NoError(t, err)

	token, err := authenticator.BuildJWTString(&auth.Claims{UserID: userID})
	require.NoError(t, err)

	ctx = metadata.NewOutgoingContext(
		ctx,
		metadata.New(map[string]string{
			"authorization": token,
		}),
	)

	urls := []string{
		"https://a.com",
		"https://b.com",
	}

	for _, u := range urls {
		_, err := client.Shorten(ctx, &pb.ShortenRequest{Url: u})
		require.NoError(t, err)
	}

	resp, err := client.GetUserURLs(ctx, &pb.GetUserURLsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Urls, len(urls))

	urlSet := map[string]bool{}
	for _, entry := range resp.Urls {
		urlSet[entry.OriginalUrl] = true
		assert.NotEmpty(t, entry.ShortUrl)
	}

	for _, u := range urls {
		assert.True(t, urlSet[u], "Expected to find %s in GetUserURLs response", u)
	}
}

func TestGetUserURLs_EmptyResult(t *testing.T) {
	client, shutdown, db, authenticator := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()

	userID, err := db.CreateUser(ctx, &user.User{}, nil)
	require.NoError(t, err)

	token, err := authenticator.BuildJWTString(&auth.Claims{UserID: userID})
	require.NoError(t, err)

	ctx = metadata.NewOutgoingContext(
		ctx,
		metadata.New(map[string]string{
			"authorization": token,
		}),
	)

	_, err = client.GetUserURLs(ctx, &pb.GetUserURLsRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestDeleteUserURLs_Success(t *testing.T) {
	client, shutdown, db, authenticator := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()

	userID, err := db.CreateUser(ctx, &user.User{}, nil)
	require.NoError(t, err)

	token, err := authenticator.BuildJWTString(&auth.Claims{UserID: userID})
	require.NoError(t, err)

	ctx = metadata.NewOutgoingContext(
		ctx,
		metadata.New(map[string]string{
			"authorization": token,
		}),
	)

	shorts := make([]string, 0)
	for _, url := range []string{"https://one.com", "https://two.com"} {
		resp, err := client.Shorten(ctx, &pb.ShortenRequest{Url: url})
		require.NoError(t, err)
		shorts = append(shorts, resp.ShortUrl)
	}

	re := regexp.MustCompile(`http://\w+:\d+/(\w+-\w+-\w+-\w+-\w+)`)
	shorts = func() []string {
		var result []string
		for _, item := range shorts {
			matches := re.FindStringSubmatch(item)
			if len(matches) == 2 {
				result = append(result, matches[1])
			}
		}
		return result
	}()

	delResp, err := client.DeleteUserURLs(ctx, &pb.DeleteUserURLsRequest{
		ShortUrls: shorts,
	})
	require.NoError(t, err)
	assert.True(t, delResp.Accepted)
}

func TestGetInternalStats_Success(t *testing.T) {
	client, shutdown, db, authenticator := startTestGRPCServer(t)
	defer shutdown()

	ctx := context.Background()
	userID1, err := db.CreateUser(ctx, &user.User{}, nil)
	require.NoError(t, err)
	token1, err := authenticator.BuildJWTString(&auth.Claims{UserID: userID1})
	require.NoError(t, err)

	userID2, err := db.CreateUser(ctx, &user.User{}, nil)
	require.NoError(t, err)
	token2, err := authenticator.BuildJWTString(&auth.Claims{UserID: userID2})
	require.NoError(t, err)

	// Shorten by userID1:

	ctx = metadata.NewOutgoingContext(
		context.Background(),
		metadata.New(map[string]string{
			"authorization": token1,
		}),
	)

	for _, u := range []string{"https://x.com", "https://y.com"} {
		_, err := client.Shorten(ctx, &pb.ShortenRequest{
			Url: u,
		})
		require.NoError(t, err)
	}

	// Shorten by userID2:

	ctx = metadata.NewOutgoingContext(
		context.Background(),
		metadata.New(map[string]string{
			"authorization": token2,
		}),
	)

	for _, u := range []string{"https://x2.com", "https://y2.com"} {
		_, err := client.Shorten(ctx, &pb.ShortenRequest{
			Url: u,
		})
		require.NoError(t, err)
	}

	resp, err := client.GetInternalStats(ctx, &pb.GetInternalStatsRequest{})
	require.NoError(t, err)
	assert.Equal(t, int64(4), resp.Urls)
	assert.Equal(t, int64(2), resp.Users)
}
