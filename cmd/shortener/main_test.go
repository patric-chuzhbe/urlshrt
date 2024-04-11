package main

import (
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

const (
	testDBFileName = "db_test.json"
)

func TestMainPageAndRedirectToFullURL(t *testing.T) {
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error

			// The DB
			theDB, err = NewSimpleJSONDB(testDBFileName)
			require.NoError(t, err)
			require.NotNil(t, theDB)
			defer func() {
				err := theDB.Close()
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
				router.Post("/", mainPage)
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
				router.Get("/{short}", redirectToFullURL)
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
