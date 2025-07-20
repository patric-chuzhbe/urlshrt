package examples

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"

	"github.com/patric-chuzhbe/urlshrt/internal/service"

	"github.com/patric-chuzhbe/urlshrt/internal/ipchecker"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/patric-chuzhbe/urlshrt/internal/config"
	"github.com/patric-chuzhbe/urlshrt/internal/db/memorystorage"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/router"

	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
)

type authenticator interface {
	AuthenticateUser(h http.Handler) http.Handler
	RegisterNewUser(h http.Handler) http.Handler
}

type userUrlsKeeper interface {
	GetUserUrls(
		ctx context.Context,
		userID string,
		shortURLFormatter models.URLFormatter,
	) (models.UserUrls, error)

	SaveUserUrls(
		ctx context.Context,
		userID string,
		urls []string,
		transaction *sql.Tx,
	) error

	GetNumberOfShortenedURLs(ctx context.Context) (int64, error)

	GetNumberOfUsers(ctx context.Context) (int64, error)
}

type transactioner interface {
	BeginTransaction() (*sql.Tx, error)

	RollbackTransaction(transaction *sql.Tx) error

	CommitTransaction(transaction *sql.Tx) error
}

type urlsMapper interface {
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

	FindFullByShort(ctx context.Context, short string) (string, bool, error)

	FindShortByFull(
		ctx context.Context,
		full string,
		transaction *sql.Tx,
	) (string, bool, error)

	InsertURLMapping(
		ctx context.Context,
		short,
		full string,
		transaction *sql.Tx,
	) error
}

type pinger interface {
	Ping(ctx context.Context) error
}

type testStorage interface {
	userUrlsKeeper
	transactioner
	urlsMapper
	pinger
	CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (string, error)
	GetUserByID(ctx context.Context, userID string, transaction *sql.Tx) (*user.User, error)
	Close() error
}

type initOptions struct {
	mockAuth bool
}

type initOption func(*initOptions)

type mockUrlsRemover struct{}

func getPostApishortenbatchRequest(amountOfURLs int) models.BatchShortenRequest {
	result := models.BatchShortenRequest{}
	for i := 0; i < amountOfURLs; i++ {
		result = append(
			result,
			models.ShortenRequestItem{
				CorrelationID: strconv.Itoa(i + 1),
				OriginalURL:   fmt.Sprintf("https://example.com/%d", i+1),
			},
		)
	}
	return result
}

func withMockAuth(value bool) initOption {
	return func(options *initOptions) {
		options.mockAuth = value
	}
}

func (m *mockUrlsRemover) EnqueueJob(job *models.URLDeleteJob) {}

func setupTestRouter(t *testing.T, optionsProto ...initOption) (*httptest.Server, testStorage, *chi.Mux) {
	options := &initOptions{}
	for _, protoOption := range optionsProto {
		protoOption(options)
	}

	cfg, err := config.New(config.WithDisableFlagsParsing(true))
	if t != nil {
		require.NoError(t, err)
	}

	db, err := memorystorage.New()
	if t != nil {
		require.NoError(t, err)
	}

	authKey, err := base64.URLEncoding.DecodeString(cfg.AuthCookieSigningSecretKey)
	if t != nil {
		require.NoError(t, err)
	}

	var authMiddleware authenticator

	if options.mockAuth {
		authMiddleware = &mockAuth{}
	} else {
		authMiddleware = auth.New(db, cfg.AuthCookieName, authKey)
	}

	ipChecker, err := ipchecker.New(cfg.TrustedSubnet)
	if t != nil {
		require.NoError(t, err)
	}

	s := service.New(
		db,
		&mockUrlsRemover{},
		cfg.ShortURLBase,
	)

	theRouter := router.New(
		db,
		authMiddleware,
		ipChecker,
		s,
	)

	err = logger.Init("debug")
	if t != nil {
		require.NoError(t, err)
	}

	return httptest.NewServer(theRouter), db, theRouter
}

type mockAuth struct{}

func (m *mockAuth) AuthenticateUser(h http.Handler) http.Handler {
	return h
}

func (m *mockAuth) RegisterNewUser(h http.Handler) http.Handler {
	return h
}

func ExampleRouter_GetPing() {
	server, _, _ := setupTestRouter(nil)
	defer server.Close()

	method := http.MethodGet
	req, err := http.NewRequest(method, server.URL+"/ping", nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	fmt.Println("Status Code:", resp.StatusCode)

	// Output:
	// Status Code: 200
}

func ExampleRouter_PostApishorten() {
	server, _, _ := setupTestRouter(nil)
	defer server.Close()

	payload := models.ShortenRequest{URL: "https://example.com"}
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/shorten", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	re := regexp.MustCompile(`\{\s*"result"\s*:\s*"http://localhost:8080/\w+-\w+-\w+-\w+-\w+"\s*\}`)

	fmt.Println("Status Code:", resp.StatusCode)
	fmt.Println("re.Match(b):", re.Match(b))

	// Output:
	// Status Code: 201
	// re.Match(b): true
}

func isGetapiuserurlsResultMatch(request models.BatchShortenRequest, response models.UserUrls) bool {
	if len(request) != len(response) {
		return false
	}

	re := regexp.MustCompile(`http://localhost:8080/\w+-\w+-\w+-\w+-\w+`)

	urlsIdx := map[string]bool{}

	for _, item := range response {
		if !re.MatchString(item.ShortURL) {
			return false
		}
		urlsIdx[item.OriginalURL] = true
	}

	for _, item := range request {
		if !urlsIdx[item.OriginalURL] {
			return false
		}
	}

	return true
}

func ExampleRouter_GetApiuserurls() {
	server, db, r := setupTestRouter(nil, withMockAuth(true))
	server.Close()

	userID, err := db.CreateUser(context.Background(), &user.User{}, nil)
	if err != nil {
		panic(err)
	}

	batchRequest := getPostApishortenbatchRequest(3)
	bodyBytes, err := json.Marshal(batchRequest)
	if err != nil {
		panic(err)
	}

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/shorten/batch", bytes.NewReader(bodyBytes))
	if err != nil {
		panic(err)
	}

	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))

	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/user/urls", nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))

	rec = httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	var result models.UserUrls
	json.NewDecoder(rec.Body).Decode(&result)

	fmt.Println("Status Code:", rec.Code)
	fmt.Println("Is GetApiuserurls() result match:", isGetapiuserurlsResultMatch(batchRequest, result))

	// Output:
	// Status Code: 200
	// Is GetApiuserurls() result match: true
}

func ExampleRouter_PostShorten() {
	server, _, _ := setupTestRouter(nil)
	defer server.Close()

	batchRequest := getPostApishortenbatchRequest(1)
	bodyBytes := []byte(batchRequest[0].OriginalURL)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/", bytes.NewReader(bodyBytes))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "plain/text")

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	re := regexp.MustCompile(`http://localhost:8080/\w+-\w+-\w+-\w+-\w+`)

	fmt.Println("Status Code:", resp.StatusCode)
	fmt.Println("re.Match(b):", re.Match(b))

	// Output:
	// Status Code: 201
	// re.Match(b): true
}

func ExampleRouter_GetRedirecttofullurl() {
	server, _, _ := setupTestRouter(nil)
	defer server.Close()

	bodyBytes := []byte("http://example.org")

	req, err := http.NewRequest(http.MethodPost, server.URL+"/", bytes.NewReader(bodyBytes))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "plain/text")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Returning http.ErrUseLastResponse tells the client to not follow redirects
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	shortURL, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	re := regexp.MustCompile(`[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}`)
	shortURLKey := re.FindString(string(shortURL))

	redirectReq, err := http.NewRequest(http.MethodGet, server.URL+"/"+shortURLKey, nil)
	if err != nil {
		panic(err)
	}

	redirectResp, err := client.Do(redirectReq)
	if err != nil {
		panic(err)
	}
	defer redirectResp.Body.Close()

	fmt.Println("Redirect Status:", redirectResp.StatusCode)
	fmt.Println("Location:", redirectResp.Header.Get("Location"))

	// Output:
	// Redirect Status: 307
	// Location: http://example.org
}
