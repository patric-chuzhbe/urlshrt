package router

import (
	"fmt"
	"github.com/go-chi/chi/v5"
	resty "github.com/go-resty/resty/v2"
	"github.com/patric-chuzhbe/urlshrt/internal/config"
	"github.com/patric-chuzhbe/urlshrt/internal/db"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	testDBFileName = "db_test.json"
)

func TestPostApishorten(t *testing.T) {
	err := config.Init(config.WithDisableFlagsParsing(true))
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

	handler := http.HandlerFunc(PostApishorten)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	err = logger.Init("debug")
	require.NoError(t, err)

	// The DB
	theDB, err = db.NewSimpleJSONDB(testDBFileName)
	require.NoError(t, err)
	require.NotNil(t, theDB)
	defer func() {
		err := theDB.SaveIntoFile()
		require.NoError(t, err)
		err = os.Remove(testDBFileName)
		require.NoError(t, err)
	}()

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

	err := config.Init()
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error

			// The DB
			theDB, err = db.NewSimpleJSONDB(testDBFileName)
			require.NoError(t, err)
			require.NotNil(t, theDB)
			defer func() {
				err := theDB.SaveIntoFile()
				require.NoError(t, err)
				err = os.Remove(testDBFileName)
				require.NoError(t, err)
			}()

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
				router.Post("/", PostShorten)
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
				router.Get("/{short}", GetRedirecttofullurl)
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
