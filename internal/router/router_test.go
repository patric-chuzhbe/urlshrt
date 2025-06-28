package router

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/patric-chuzhbe/urlshrt/internal/mockstorage"

	"github.com/go-chi/chi/v5"
	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"

	"github.com/patric-chuzhbe/urlshrt/internal/db/jsondb"
	"github.com/patric-chuzhbe/urlshrt/internal/db/postgresdb"
	"github.com/patric-chuzhbe/urlshrt/internal/gzippedhttp"

	"github.com/stretchr/testify/require"

	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	"github.com/patric-chuzhbe/urlshrt/internal/config"
	"github.com/patric-chuzhbe/urlshrt/internal/db/memorystorage"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
)

const (
	testDBFileName = "db_test.json"
	databaseDSN    = "" // host=localhost user=video password=x7lKzhrpL8E9LsZ4rQfXnk3pJutOQV dbname=videos sslmode=disable
	migrationsDir  = `../../cmd/shortener/migrations`
)

type testStorage interface {
	storage
	CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (string, error)
	GetUserByID(ctx context.Context, userID string, transaction *sql.Tx) (*user.User, error)
	Close() error
}

type mockAuth struct{}

func (m *mockAuth) AuthenticateUser(h http.Handler) http.Handler {
	return h
}

func (m *mockAuth) RegisterNewUser(h http.Handler) http.Handler {
	return h
}

type initOption func(*initOptions)

type initOptions struct {
	mockAuth    bool
	mockStorage testStorage
}

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

func gzipString(input string) ([]byte, error) {
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)

	_, err := gzipWriter.Write([]byte(input))
	if err != nil {
		return nil, err
	}

	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func TestPostApishortenForGzip(t *testing.T) {
	cfg, err := config.New(config.WithDisableFlagsParsing(true))
	require.NoError(t, err)

	type tRequest struct {
		method string
		body   []byte
	}
	type tExpectedResponse struct {
		code int
		body *regexp.Regexp
	}
	type tTestCase struct {
		name             string
		request          tRequest
		expectedResponse tExpectedResponse
	}
	positiveRequestBody := `{
		"url": "https://ru.wikipedia.org/wiki/%D0%9F%D1%83%D1%88%D0%BA%D0%B0"
	}`
	firstTestCaseBody, err := gzipString(positiveRequestBody)
	if err != nil {
		log.Fatal(err)
	}
	testCases := []tTestCase{
		{
			name: "positive",
			request: tRequest{
				http.MethodPost,
				firstTestCaseBody,
			},
			expectedResponse: tExpectedResponse{
				http.StatusCreated,
				regexp.MustCompile(`\{\s*"result"\s*:\s*"http://localhost:8080/\w+-\w+-\w+-\w+-\w+"\s*\}`),
			},
		},
	}

	// The DB
	db, err := jsondb.New(testDBFileName)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer func() {
		err := db.Close()
		require.NoError(t, err)
		err = os.Remove(testDBFileName)
		require.NoError(t, err)
	}()

	myRouter := Router{
		db:           db,
		shortURLBase: cfg.ShortURLBase,
	}

	authCookieSigningSecretKey, err := base64.URLEncoding.DecodeString(cfg.AuthCookieSigningSecretKey)
	require.NoError(t, err)
	theAuth := auth.New(
		db,
		cfg.AuthCookieName,
		authCookieSigningSecretKey,
	)

	router := chi.NewRouter()
	router.Use(
		logger.WithLoggingHTTPMiddleware,
		gzippedhttp.UngzipJSONAndTextHTMLRequest,
	)
	router.With(
		gzippedhttp.GzipResponse,
		theAuth.AuthenticateUser,
		theAuth.RegisterNewUser,
	).Post(`/api/shorten`, myRouter.PostApishorten)

	srv := httptest.NewServer(router)
	defer srv.Close()

	err = logger.Init("debug")
	require.NoError(t, err)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			req := resty.New().R()
			req.Method = testCase.request.method
			req.URL = fmt.Sprintf("%s/api/shorten", srv.URL)

			if len(testCase.request.body) > 0 {
				req.SetHeader("Content-Type", "application/json")
				req.SetHeader("Content-Encoding", "gzip")
				req.SetHeader("Accept-Encoding", "gzip")
				req.SetBody(testCase.request.body)
			}

			resp, err := req.Send()
			assert.NoError(t, err, "error making HTTP request")

			assert.Equal(t, testCase.expectedResponse.code, resp.StatusCode(), "Response code didn't match expected value")

			if testCase.expectedResponse.body != nil {
				assert.NotNil(
					t,
					testCase.expectedResponse.body.FindIndex(resp.Body()),
					fmt.Sprintf(
						"The response body should match expected value (%s)",
						testCase.expectedResponse.body.String(),
					),
				)
			}
		})
	}
}

func TestPostApishorten(t *testing.T) {
	cfg, err := config.New(config.WithDisableFlagsParsing(true))
	require.NoError(t, err)

	type tRequest struct {
		method string
		body   string
	}
	type tExpectedResponse struct {
		code int
		body *regexp.Regexp
	}
	type tTestCase struct {
		name             string
		request          tRequest
		expectedResponse tExpectedResponse
	}
	positiveRequestBody := `{
		"url": "https://ru.wikipedia.org/wiki/%D0%9F%D1%83%D1%88%D0%BA%D0%B0"
	}`
	testCases := []tTestCase{
		{
			name: "positive",
			request: tRequest{
				http.MethodPost,
				positiveRequestBody,
			},
			expectedResponse: tExpectedResponse{
				http.StatusCreated,
				regexp.MustCompile(`\{\s*"result"\s*:\s*"http://localhost:8080/\w+-\w+-\w+-\w+-\w+"\s*\}`),
			},
		},
		{
			name: "empty_JSON",
			request: tRequest{
				http.MethodPost,
				`{}`,
			},
			expectedResponse: tExpectedResponse{
				http.StatusUnprocessableEntity,
				nil,
			},
		},
		{
			name: "empty_body",
			request: tRequest{
				http.MethodPost,
				``,
			},
			expectedResponse: tExpectedResponse{
				http.StatusInternalServerError,
				nil,
			},
		},
		{
			name: "unsupported_method_get",
			request: tRequest{
				http.MethodGet,
				positiveRequestBody,
			},
			expectedResponse: tExpectedResponse{
				http.StatusMethodNotAllowed,
				nil,
			},
		},
		{
			name: "unsupported_method_put",
			request: tRequest{
				http.MethodPut,
				``,
			},
			expectedResponse: tExpectedResponse{
				http.StatusMethodNotAllowed,
				nil,
			},
		},
	}

	// The DB
	theDB, err := jsondb.New(testDBFileName)
	require.NoError(t, err)
	require.NotNil(t, theDB)
	defer func() {
		err := theDB.Close()
		require.NoError(t, err)
		err = os.Remove(testDBFileName)
		require.NoError(t, err)
	}()

	authCookieSigningSecretKey, err := base64.URLEncoding.DecodeString(cfg.AuthCookieSigningSecretKey)
	require.NoError(t, err)
	theAuth := auth.New(
		theDB,
		cfg.AuthCookieName,
		authCookieSigningSecretKey,
	)

	myRouter := Router{
		db:           theDB,
		shortURLBase: cfg.ShortURLBase,
	}

	router := chi.NewRouter()
	router.With(
		theAuth.AuthenticateUser,
		theAuth.RegisterNewUser,
	).Post(`/api/shorten`, myRouter.PostApishorten)

	srv := httptest.NewServer(router)
	defer srv.Close()

	err = logger.Init("debug")
	require.NoError(t, err)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			req := resty.New().R()
			req.Method = testCase.request.method
			req.URL = fmt.Sprintf("%s/api/shorten", srv.URL)

			if len(testCase.request.body) > 0 {
				req.SetHeader("Content-Type", "application/json")
				req.SetBody(testCase.request.body)
			}

			resp, err := req.Send()
			assert.NoError(t, err, "error making HTTP request")

			assert.Equal(t, testCase.expectedResponse.code, resp.StatusCode(), "Response code didn't match expected value")

			if testCase.expectedResponse.body != nil {
				assert.NotNil(
					t,
					testCase.expectedResponse.body.FindIndex(resp.Body()),
					fmt.Sprintf(
						"The response body should match expected value (%s)",
						testCase.expectedResponse.body.String(),
					),
				)
			}
		})
	}
}

func TestPostShortenAndGetRedirecttofullurl(t *testing.T) {
	type requestResult struct {
		bypass     bool
		statusCode int
		location   string
	}
	type want struct {
		mainPageRequestResult          requestResult
		redirectToFullURLRequestResult requestResult
	}
	type request struct {
		bypass bool
		URL    string
		body   string
	}
	tests := []struct {
		name                     string
		mainPageRequest          request
		redirectToFullURLRequest request
		want                     want
	}{
		{
			name: "positive test case",
			mainPageRequest: request{
				URL:  "/",
				body: "https://ru.wikipedia.org/wiki/Go",
			},
			want: want{
				mainPageRequestResult: requestResult{
					statusCode: http.StatusCreated,
				},
				redirectToFullURLRequestResult: requestResult{
					statusCode: http.StatusTemporaryRedirect,
					location:   "https://ru.wikipedia.org/wiki/Go",
				},
			},
		},
		{
			name: "incorrect URL",
			mainPageRequest: request{
				URL:  "/",
				body: "h t t p s://ru.wikipedia.org/wiki/Go",
			},
			redirectToFullURLRequest: request{
				bypass: true,
			},
			want: want{
				mainPageRequestResult: requestResult{
					statusCode: http.StatusBadRequest,
				},
				redirectToFullURLRequestResult: requestResult{
					bypass: true,
				},
			},
		},
		{
			name: "request the redirection to nonexistent short URL",
			mainPageRequest: request{
				bypass: true,
			},
			redirectToFullURLRequest: request{
				URL: "http://localhost:8080/NONEXISTENT",
			},
			want: want{
				redirectToFullURLRequestResult: requestResult{
					statusCode: http.StatusNotFound,
				},
			},
		},
		{
			name: "request for shorten with few strings and URL somewhere in the middle",
			mainPageRequest: request{
				URL: "/",
				body: `
string
some incorrect URL
h t t ps://ru.wikipedia.org/wiki/%D0%93%D1%83%D0%BC%D0%B0%D0%BD%D0%BE%D0%B8%D0%B4
https://ru.wikipedia.org/wiki/%D0%A7%D0%B5%D0%BB%D0%BE%D0%B2%D0%B5%D0%BA
eshche odna stroka

`,
			},
			want: want{
				mainPageRequestResult: requestResult{
					statusCode: http.StatusCreated,
				},
				redirectToFullURLRequestResult: requestResult{
					statusCode: http.StatusTemporaryRedirect,
					location:   "https://ru.wikipedia.org/wiki/%D0%A7%D0%B5%D0%BB%D0%BE%D0%B2%D0%B5%D0%BA",
				},
			},
		},
	}

	cfg, err := config.New()
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error

			// The DB
			theDB, err := jsondb.New(testDBFileName)
			require.NoError(t, err)
			require.NotNil(t, theDB)
			defer func() {
				err := theDB.Close()
				require.NoError(t, err)
				err = os.Remove(testDBFileName)
				require.NoError(t, err)
			}()

			myRouter := Router{
				db:           theDB,
				shortURLBase: cfg.ShortURLBase,
			}

			var shortURL []byte

			// Shorten URL
			if !tt.mainPageRequest.bypass {
				request := httptest.NewRequest(
					http.MethodPost,
					tt.mainPageRequest.URL,
					strings.NewReader(tt.mainPageRequest.body),
				)
				w := httptest.NewRecorder()
				router := chi.NewRouter()

				authCookieSigningSecretKey, err := base64.URLEncoding.DecodeString(cfg.AuthCookieSigningSecretKey)
				require.NoError(t, err)
				theAuth := auth.New(
					theDB,
					cfg.AuthCookieName,
					authCookieSigningSecretKey,
				)

				router.With(
					theAuth.AuthenticateUser,
					theAuth.RegisterNewUser,
				).Post("/", myRouter.PostShorten)
				router.ServeHTTP(w, request)

				result := w.Result()

				assert.Equal(t, tt.want.mainPageRequestResult.statusCode, result.StatusCode)

				shortURL, err = io.ReadAll(result.Body)
				require.NoError(t, err)

				err = result.Body.Close()
				require.NoError(t, err)
			}

			// Redirect to full URL
			if !tt.redirectToFullURLRequest.bypass {
				if tt.mainPageRequest.bypass {
					shortURL = []byte(tt.redirectToFullURLRequest.URL)
				}

				_, err := url.Parse(string(shortURL))
				require.NoError(t, err)

				request := httptest.NewRequest(
					http.MethodGet,
					string(shortURL),
					nil,
				)

				w := httptest.NewRecorder()
				router := chi.NewRouter()
				router.Get("/{short}", myRouter.GetRedirecttofullurl)
				router.ServeHTTP(w, request)

				result := w.Result()

				assert.Equal(t, tt.want.redirectToFullURLRequestResult.statusCode, result.StatusCode)
				assert.Equal(t, tt.want.redirectToFullURLRequestResult.location, result.Header.Get("Location"))

				err = result.Body.Close()
				require.NoError(t, err)
			}
		})
	}
}

type mockUrlsRemover struct {
	jobs []*models.URLDeleteJob
}

func (m *mockUrlsRemover) EnqueueJob(job *models.URLDeleteJob) {
	m.jobs = append(m.jobs, job)
}

func BenchmarkPostApishortenbatch(b *testing.B) {
	cfg, err := config.New(config.WithDisableFlagsParsing(true))
	require.NoError(b, err)

	var db testStorage
	if databaseDSN != "" {
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
	require.NoError(b, err)
	defer func() {
		_ = db.Close()
	}()

	authCookieSigningSecretKey, err := base64.URLEncoding.DecodeString(cfg.AuthCookieSigningSecretKey)
	require.NoError(b, err)
	theAuth := auth.New(
		db,
		cfg.AuthCookieName,
		authCookieSigningSecretKey,
	)

	err = logger.Init("debug")
	require.NoError(b, err)

	theRouter := New(
		db,
		cfg.ShortURLBase,
		theAuth,
		&mockUrlsRemover{},
	)

	server := httptest.NewServer(theRouter)
	defer server.Close()

	batchRequest := getPostApishortenbatchRequest(100)
	bodyBytes, err := json.Marshal(batchRequest)
	require.NoError(b, err)

	client := &http.Client{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/shorten/batch", bytes.NewReader(bodyBytes))
		require.NoError(b, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		require.NoError(b, err)
		err = resp.Body.Close()
		require.NoError(b, err)
	}
}

func withMockStorage(db testStorage) initOption {
	return func(options *initOptions) {
		options.mockStorage = db
	}
}

func withMockAuth(value bool) initOption {
	return func(options *initOptions) {
		options.mockAuth = value
	}
}

func setupTestRouter(t *testing.T, optionsProto ...initOption) (*httptest.Server, testStorage, *chi.Mux, *mockUrlsRemover) {
	options := &initOptions{}
	for _, protoOption := range optionsProto {
		protoOption(options)
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

	urlsRemover := &mockUrlsRemover{}

	theRouter := New(
		db,
		cfg.ShortURLBase,
		authMiddleware,
		urlsRemover,
	)

	err = logger.Init("debug")
	if t != nil {
		require.NoError(t, err)
	}

	return httptest.NewServer(theRouter), db, theRouter, urlsRemover
}

func TestPostApishortenbatch(t *testing.T) {
	server, db, _, _ := setupTestRouter(t)
	defer server.Close()

	type requestItem struct {
		CorrelationID string `json:"correlation_id"`
		OriginalURL   string `json:"original_url"`
	}
	type responseItem struct {
		CorrelationID string `json:"correlation_id"`
		ShortURL      string `json:"short_url"`
	}

	tests := []struct {
		name               string
		requestBody        string
		expectedStatusCode int
		assertionLogic     func(t *testing.T, req []requestItem, resp []responseItem)
	}{
		{
			name: "valid batch",
			requestBody: `[
				{"correlation_id":"1", "original_url":"https://example.com/1"},
				{"correlation_id":"2", "original_url":"https://example.com/2"}
			]`,
			expectedStatusCode: http.StatusCreated,
			assertionLogic: func(t *testing.T, req []requestItem, resp []responseItem) {
				require.Len(t, resp, len(req))
				for _, r := range resp {
					assert.NotEmpty(t, r.CorrelationID)
					assert.NotEmpty(t, r.ShortURL)

					fullURL, ok, err := db.FindFullByShort(
						context.Background(),
						strings.TrimPrefix(r.ShortURL, "http://localhost:8080/"),
					)
					require.NoError(t, err)
					require.True(t, ok)

					var original string
					for _, i := range req {
						if i.CorrelationID == r.CorrelationID {
							original = i.OriginalURL
							break
						}
					}
					assert.Equal(t, original, fullURL)
				}
			},
		},
		{
			name: "duplicate URLs in batch",
			requestBody: `[
				{"correlation_id":"1", "original_url":"https://example.com/dup"},
				{"correlation_id":"2", "original_url":"https://example.com/dup"}
			]`,
			expectedStatusCode: http.StatusCreated,
			assertionLogic: func(t *testing.T, req []requestItem, resp []responseItem) {
				for _, r := range resp {
					assert.NotEmpty(t, r.CorrelationID)
					assert.NotEmpty(t, r.ShortURL)

					fullURL, ok, err := db.FindFullByShort(
						context.Background(),
						strings.TrimPrefix(r.ShortURL, "http://localhost:8080/"),
					)
					require.NoError(t, err)
					require.True(t, ok)

					var original string
					for _, i := range req {
						if i.CorrelationID == r.CorrelationID {
							original = i.OriginalURL
							break
						}
					}
					assert.Equal(t, original, fullURL)
				}
			},
		},
		{
			name:               "invalid JSON",
			requestBody:        `[{correlation_id:1, original_url:"noquote.com"}]`,
			expectedStatusCode: http.StatusInternalServerError,
			assertionLogic:     nil,
		},
		{
			name:               "empty body",
			requestBody:        ``,
			expectedStatusCode: http.StatusInternalServerError,
			assertionLogic:     nil,
		},
		{
			name:               "wrong method",
			requestBody:        `[]`,
			expectedStatusCode: http.StatusMethodNotAllowed,
			assertionLogic:     nil,
		},
	}

	client := &http.Client{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := http.MethodPost
			if tt.name == "wrong method" {
				method = http.MethodGet
			}
			req, err := http.NewRequest(method, server.URL+"/api/shorten/batch", strings.NewReader(tt.requestBody))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatusCode, resp.StatusCode)

			if tt.assertionLogic != nil {
				var decodedResp []responseItem
				err := json.NewDecoder(resp.Body).Decode(&decodedResp)
				require.NoError(t, err)

				var decodedReq []requestItem
				err = json.Unmarshal([]byte(tt.requestBody), &decodedReq)
				require.NoError(t, err)

				tt.assertionLogic(t, decodedReq, decodedResp)
			}
		})
	}
}

func TestDeleteApiuserurls(t *testing.T) {
	server, db, r, urlsRemover := setupTestRouter(t, withMockAuth(true))
	server.Close()

	userID, err := db.CreateUser(context.Background(), &user.User{}, nil)
	require.NoError(t, err)

	t.Run("positive case - valid user and URLs", func(t *testing.T) {
		batchRequest := getPostApishortenbatchRequest(3)
		bodyBytes, err := json.Marshal(batchRequest)
		require.NoError(t, err)

		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/shorten/batch", bytes.NewReader(bodyBytes))
		require.NoError(t, err)

		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))

		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var postAPIShortenBatchResult models.UserUrls
		err = json.NewDecoder(rec.Body).Decode(&postAPIShortenBatchResult)
		require.NoError(t, err)

		re := regexp.MustCompile(`http://\w+:\d+/(\w+-\w+-\w+-\w+-\w+)`)
		urls := func() models.DeleteURLsRequest {
			var result models.DeleteURLsRequest
			for _, item := range postAPIShortenBatchResult {
				matches := re.FindStringSubmatch(item.ShortURL)
				if len(matches) == 2 {
					result = append(result, matches[1])
				}
			}
			return result
		}()

		body, err := json.Marshal(urls)
		require.NoError(t, err)
		req, err = http.NewRequest(http.MethodDelete, server.URL+"/api/user/urls", bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))

		rec = httptest.NewRecorder()

		r.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusAccepted, rec.Code)
		assert.Equal(t, 1, len(urlsRemover.jobs))
	})

	t.Run("unauthorized - missing user ID in context", func(t *testing.T) {
		body, err := json.Marshal(models.DeleteURLsRequest([]string{"abc"}))
		require.NoError(t, err)
		req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/user/urls", bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("internal error - invalid payload structure", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/user/urls", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("internal error - malformed JSON", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/user/urls", strings.NewReader(`[{malformed json`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestGetApiuserurls(t *testing.T) {
	server, db, r, _ := setupTestRouter(t, withMockAuth(true))
	defer server.Close()

	userID, err := db.CreateUser(context.Background(), &user.User{}, nil)
	require.NoError(t, err)

	t.Run("ok: user with multiple URLs", func(t *testing.T) {
		batchRequest := getPostApishortenbatchRequest(3)
		bodyBytes, err := json.Marshal(batchRequest)
		require.NoError(t, err)

		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/shorten/batch", bytes.NewReader(bodyBytes))
		require.NoError(t, err)

		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))

		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusCreated, rec.Code)
		var postAPIShortenBatchResult models.UserUrls
		err = json.NewDecoder(rec.Body).Decode(&postAPIShortenBatchResult)
		require.NoError(t, err)

		req = httptest.NewRequest(http.MethodGet, "/api/user/urls", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var result models.UserUrls
		err = json.NewDecoder(rec.Body).Decode(&result)
		require.NoError(t, err)
		assert.Len(t, result, 3)
	})

	t.Run("empty result: user exists but no URLs", func(t *testing.T) {
		userID, err := db.CreateUser(context.Background(), &user.User{}, nil)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/user/urls", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("unauthorized: no user in context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/user/urls", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("internal error in the db.GetUserUrls() method", func(t *testing.T) {
		db := new(mockstorage.StorageMock)
		server, _, r, _ := setupTestRouter(t, withMockAuth(true), withMockStorage(db))
		defer server.Close()

		db.On(
			"GetUserUrls",
			mock.Anything,
			userID,
			mock.Anything,
		).
			Return(
				models.UserUrls(nil),
				errors.New("db error"),
			)

		req := httptest.NewRequest(http.MethodGet, "/api/user/urls", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}
