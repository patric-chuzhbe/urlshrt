package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"

	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
)

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
	server, db, router := setupTestRouter(nil, withMockAuth(true))
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

	router.ServeHTTP(rec, req)

	req, err = http.NewRequest(http.MethodGet, server.URL+"/api/user/urls", nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))

	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

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
