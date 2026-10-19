package router

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/patric-chuzhbe/urlshrt/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"

	gzippedHttp "github.com/patric-chuzhbe/urlshrt/internal/gzippedhttp"

	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
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

type storage interface {
	userUrlsKeeper
	transactioner
	urlsMapper
	pinger
}

type ipChecker interface {
	IsTrustedSubnetEmpty() bool

	GetClientIP(request *http.Request) (net.IP, error)

	Check(clientIP net.IP) bool
}

// Router defines the application's HTTP router, which handles incoming requests
// for the URL shortening service. It encapsulates the mux router and its associated
// dependencies such as storage, authentication, and logger middleware.
//
// It provides handlers for shortening URLs, retrieving user-specific URLs,
// deleting URLs, and redirecting short URLs to their full versions.
type Router struct {
	db        storage
	validator *validator.Validate
	ipChecker ipChecker
	service   *service.Service
}

// New initializes and returns a new HTTP Router with middleware and handlers.
func New(
	database storage,
	auth authenticator,
	ipChecker ipChecker,
	service *service.Service,
) *chi.Mux {
	myRouter := Router{
		db:        database,
		ipChecker: ipChecker,
		service:   service,
	}
	router := chi.NewRouter()

	router.Use(
		logger.WithLoggingHTTPMiddleware,
		gzippedHttp.UngzipJSONAndTextHTMLRequest,
	)

	router.With(
		gzippedHttp.GzipResponse,
		auth.AuthenticateUser,
		auth.RegisterNewUser,
	).Post(`/`, myRouter.PostShorten)

	router.Get(`/{short}`, myRouter.GetRedirecttofullurl)

	router.With(
		gzippedHttp.GzipResponse,
		auth.AuthenticateUser,
		auth.RegisterNewUser,
	).Post(`/api/shorten`, myRouter.PostApishorten)

	router.Get(`/ping`, myRouter.GetPing)

	router.With(
		gzippedHttp.GzipResponse,
		auth.AuthenticateUser,
		auth.RegisterNewUser,
	).Post(`/api/shorten/batch`, myRouter.PostApishortenbatch)

	router.With(
		auth.AuthenticateUser,
		auth.RegisterNewUser,
	).Get(`/api/user/urls`, myRouter.GetApiuserurls)

	router.With(
		auth.AuthenticateUser,
	).Delete(`/api/user/urls`, myRouter.DeleteApiuserurls)

	router.With(gzippedHttp.GzipResponse).Get(`/api/internal/stats`, myRouter.GetApiinternalstats)

	return router
}

// DeleteApiuserurls asynchronously enqueues a job to delete user-owned URLs.
// Responds with 202 if accepted or 401/422/500 on error.
func (theRouter Router) DeleteApiuserurls(response http.ResponseWriter, request *http.Request) {
	userID, ok := request.Context().Value(auth.UserIDKey).(string)
	if !ok || userID == "" {
		response.WriteHeader(http.StatusUnauthorized)
		return
	}

	var urls models.DeleteURLsRequest
	if err := json.NewDecoder(request.Body).Decode(&urls); err != nil {
		logger.Log.Debugln("cannot decode request JSON body", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	validate := validator.New()
	if err := validate.Var(urls, "dive"); err != nil {
		logger.Log.Debugln("incorrect request structure", zap.Error(err))
		response.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	theRouter.service.DeleteURLsAsync(request.Context(), userID, urls)

	response.WriteHeader(http.StatusAccepted)
}

// GetApiuserurls returns all user-specific shortened URLs in JSON format.
// Responds with 200 and the list or 204 if no URLs exist.
func (theRouter Router) GetApiuserurls(response http.ResponseWriter, request *http.Request) {
	userID, ok := request.Context().Value(auth.UserIDKey).(string)
	if !ok || userID == "" {
		response.WriteHeader(http.StatusUnauthorized)
		return
	}

	responseDTO, err := theRouter.service.GetUserURLs(request.Context(), userID)
	if err != nil {
		logger.Log.Debugln("Error calling the `service.GetUserURLs()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	if len(responseDTO) == 0 {
		response.WriteHeader(http.StatusNoContent)
		return
	}

	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(responseDTO); err != nil {
		logger.Log.Debug("error encoding response", zap.Error(err))
	}
}

// PostApishortenbatch handles batch URL shortening via API.
// Accepts a list of URLs and returns their short mappings.
func (theRouter Router) PostApishortenbatch(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		logger.Log.Debug("got request with bad method", zap.String("method", request.Method))
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var requestDTO models.BatchShortenRequest
	if err := json.NewDecoder(request.Body).Decode(&requestDTO); err != nil {
		logger.Log.Debugln("cannot decode request JSON body", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	validate := validator.New()
	if err := validate.Var(requestDTO, "dive"); err != nil {
		logger.Log.Debugln("incorrect request structure", zap.Error(err))
		response.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	userID, ok := request.Context().Value(auth.UserIDKey).(string)
	if !ok || userID == "" {
		response.WriteHeader(http.StatusUnauthorized)
		return
	}

	batchResp, err := theRouter.service.BatchShortenURLs(request.Context(), requestDTO, userID)
	if err != nil {
		logger.Log.Debugln("error during batch shortening", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(response).Encode(batchResp); err != nil {
		logger.Log.Debug("error encoding response", zap.Error(err))
		return
	}
}

// GetPing is a healthcheck handler that returns 200 OK if the DB is reachable.
func (theRouter Router) GetPing(response http.ResponseWriter, request *http.Request) {
	err := theRouter.service.Ping(request.Context())
	if err != nil {
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
	response.WriteHeader(http.StatusOK)
}

// PostApishorten handles API requests to shorten a single URL.
// Accepts a JSON body and responds with a JSON containing the short URL.
func (theRouter Router) PostApishorten(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		logger.Log.Debug("got request with bad method", zap.String("method", request.Method))
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var requestDTO models.ShortenRequest
	if err := json.NewDecoder(request.Body).Decode(&requestDTO); err != nil {
		logger.Log.Debugln("cannot decode request JSON body", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	validate := validator.New()
	if err := validate.Struct(requestDTO); err != nil {
		logger.Log.Debugln("incorrect request structure", zap.Error(err))
		response.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	userID, ok := request.Context().Value(auth.UserIDKey).(string)
	if !ok {
		logger.Log.Debugln("The `userID` value was not found in the request's context")
		response.WriteHeader(http.StatusUnauthorized)
		return
	}

	urlToShort := requestDTO.URL
	shortURL, err := theRouter.service.ShortenURL(request.Context(), urlToShort, userID)
	if err != nil && !errors.Is(err, service.ErrConflict) {
		logger.Log.Debugln("error while `theRouter.getShortKey()` calling: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	responseDTO := models.ShortenResponse{Result: shortURL}

	response.Header().Set("Content-Type", "application/json")

	resultStatus := http.StatusCreated
	if errors.Is(err, service.ErrConflict) {
		resultStatus = http.StatusConflict
	}
	response.WriteHeader(resultStatus)

	if err := json.NewEncoder(response).Encode(responseDTO); err != nil {
		logger.Log.Debug("error encoding response", zap.Error(err))
		return
	}
}

// GetRedirecttofullurl redirects short URLs to their original URL if found.
// Responds with 307 Temporary Redirect or 404 if not found.
func (theRouter Router) GetRedirecttofullurl(res http.ResponseWriter, req *http.Request) {
	short := chi.URLParam(req, "short")
	full, err := theRouter.service.GetOriginalURL(req.Context(), short)
	if errors.Is(err, service.ErrURLMarkedAsDeleted) {
		res.WriteHeader(http.StatusGone)
		return
	}
	if err != nil {
		logger.Log.Debugln("error while resolving short URL: ", zap.Error(err))
		res.WriteHeader(http.StatusInternalServerError)
		return
	}
	if full == "" {
		res.WriteHeader(http.StatusNotFound)
		return
	}
	http.Redirect(res, req, full, http.StatusTemporaryRedirect)
}

// PostShorten handles plain text full URL.
// Responds with a plain text short URL or 409 on conflict.
func (theRouter Router) PostShorten(response http.ResponseWriter, request *http.Request) {
	urlToShort, err := theRouter.getURLToShort(request)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}

	userID, ok := request.Context().Value(auth.UserIDKey).(string)
	if !ok {
		logger.Log.Debugln("The `userID` value was not found in the request's context")
		response.WriteHeader(http.StatusUnauthorized)
		return
	}

	shortURL, err := theRouter.service.ShortenURL(request.Context(), urlToShort, userID)
	if err != nil && !errors.Is(err, service.ErrConflict) {
		logger.Log.Debugln("error while shortening URL: ", zap.Error(err))
		http.Error(response, err.Error(), http.StatusInternalServerError)
		return
	}

	status := http.StatusCreated
	if errors.Is(err, service.ErrConflict) {
		status = http.StatusConflict
	}
	response.WriteHeader(status)

	_, err = response.Write([]byte(shortURL))
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
}

// GetApiinternalstats handles the GET /api/internal/stats endpoint,
// which returns internal metrics such as the total number of shortened URLs
// and the number of registered users in the system.
//
// Access to this endpoint is restricted to requests originating from
// a trusted subnet. The client IP is extracted from standard headers
// like X-Real-IP or X-Forwarded-For, and validated against the configured
// trusted subnet.
//
// If the client is from the trusted subnet, the handler responds with a JSON payload
// containing system statistics. Otherwise, it returns 403 Forbidden
// or an appropriate error code for invalid requests.
func (theRouter Router) GetApiinternalstats(response http.ResponseWriter, request *http.Request) {
	if theRouter.ipChecker.IsTrustedSubnetEmpty() {
		response.WriteHeader(http.StatusForbidden)
		return
	}

	clientIP, err := theRouter.ipChecker.GetClientIP(request)
	if err != nil || string(clientIP) == "" {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	if !theRouter.ipChecker.Check(clientIP) {
		response.WriteHeader(http.StatusForbidden)
		return
	}

	stats, err := theRouter.service.GetInternalStats(request.Context())
	if err != nil {
		logger.Log.Debugln("error fetching internal stats: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(stats); err != nil {
		logger.Log.Debug("error encoding internal stats response", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
	}
}

func (theRouter Router) getURLToShort(req *http.Request) (string, error) {
	urlToShort, err := io.ReadAll(req.Body)
	if err != nil {
		return "", err
	}

	urlToShortAsString := string(urlToShort)

	urlToShortAsString, err = theRouter.service.ExtractFirstURL(urlToShortAsString)
	if err != nil {
		return "", err
	}

	return urlToShortAsString, nil
}
