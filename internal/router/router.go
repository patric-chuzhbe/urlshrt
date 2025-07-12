package router

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/thoas/go-funk"
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

type urlsRemover interface {
	EnqueueJob(job *models.URLDeleteJob)
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
	db           storage
	shortURLBase string
	urlsRemover  urlsRemover
	validator    *validator.Validate
	ipChecker    ipChecker
}

var urlPattern = regexp.MustCompile(`\bhttps?://\S+\b`)

// ErrConflict is returned when a short URL already exists for the provided original URL.
var ErrConflict = errors.New("data conflict")

// New initializes and returns a new HTTP Router with middleware and handlers.
func New(
	database storage,
	shortURLBase string,
	auth authenticator,
	urlsRemover urlsRemover,
	ipChecker ipChecker,
) *chi.Mux {
	myRouter := Router{
		db:           database,
		shortURLBase: shortURLBase,
		urlsRemover:  urlsRemover,
		ipChecker:    ipChecker,
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

	var URLsToDelete models.DeleteURLsRequest
	if err := json.NewDecoder(request.Body).Decode(&URLsToDelete); err != nil {
		logger.Log.Debugln("cannot decode request JSON body", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	validate := validator.New()
	if err := validate.Var(URLsToDelete, "dive"); err != nil {
		logger.Log.Debugln("incorrect request structure", zap.Error(err))
		response.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	theRouter.urlsRemover.EnqueueJob(&models.URLDeleteJob{
		UserID:       userID,
		URLsToDelete: URLsToDelete,
	})

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
	responseDTO, err := theRouter.db.GetUserUrls(request.Context(), userID, theRouter.getShortURL)
	if err != nil {
		logger.Log.Debugln("Error calling the `theRouter.db.GetUserUrls()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)

		return
	}

	if len(responseDTO) == 0 {
		response.WriteHeader(http.StatusNoContent)

		return
	}

	response.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(response).Encode(responseDTO)
	if err != nil {
		logger.Log.Debug("error encoding response", zap.Error(err))

		return
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

	transaction, err := theRouter.db.BeginTransaction()
	if err != nil {
		logger.Log.Debugln("Error calling the `theRouter.db.BeginTransaction()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)

		return
	}

	originalURLToCorrelationIDMap := theRouter.getOriginalURLToCorrelationIDMap(requestDTO)

	originalUrls := funk.Keys(originalURLToCorrelationIDMap).([]string)

	existentFullsToShortsMap, err := theRouter.db.FindShortsByFulls(request.Context(), originalUrls, transaction)
	if err != nil {
		err2 := theRouter.db.RollbackTransaction(transaction)
		if err2 != nil {
			logger.Log.Debugln("Error calling the `theRouter.db.RollbackTransaction()`: ", zap.Error(err2))
		}
		logger.Log.Debugln("Error calling the `theRouter.db.FindShortsByFulls()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)

		return
	}

	existentFulls := funk.Keys(existentFullsToShortsMap).([]string)
	unexistentFulls := differenceStringSlices(originalUrls, existentFulls)
	unexistentFullsToShortsMap := theRouter.getUnexistentFullsToShortsMap(unexistentFulls)
	err = theRouter.db.SaveNewFullsAndShorts(request.Context(), unexistentFullsToShortsMap, transaction)
	if err != nil {
		err2 := theRouter.db.RollbackTransaction(transaction)
		if err2 != nil {
			logger.Log.Debugln("Error calling the `theRouter.db.RollbackTransaction()`: ", zap.Error(err2))
		}
		logger.Log.Debugln("Error calling the `theRouter.db.SaveNewFullsAndShorts()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)

		return
	}

	userID, ok := request.Context().Value(auth.UserIDKey).(string)
	if !ok {
		logger.Log.Debugln("The `userID` value was not found in the request's context")
		response.WriteHeader(http.StatusUnauthorized)

		return
	}

	err = theRouter.db.SaveUserUrls(
		request.Context(),
		userID,
		funk.Uniq(funk.Union(existentFulls, unexistentFulls)).([]string),
		transaction,
	)
	if err != nil {
		err2 := theRouter.db.RollbackTransaction(transaction)
		if err2 != nil {
			logger.Log.Debugln("Error calling the `theRouter.db.RollbackTransaction()`: ", zap.Error(err2))
		}
		logger.Log.Debugln("Error calling the `theRouter.db.SaveUserUrls()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)

		return
	}

	responseDTO := theRouter.getPostApishortenbatchResponse(
		existentFullsToShortsMap,
		unexistentFullsToShortsMap,
		originalURLToCorrelationIDMap,
	)

	err = theRouter.db.CommitTransaction(transaction)
	if err != nil {
		logger.Log.Debugln("Error calling the `theRouter.db.CommitTransaction()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)

		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(response).Encode(responseDTO); err != nil {
		logger.Log.Debug("error encoding response", zap.Error(err))

		return
	}
}

// GetPing is a healthcheck handler that returns 200 OK if the DB is reachable.
func (theRouter Router) GetPing(response http.ResponseWriter, request *http.Request) {
	err := theRouter.db.Ping(request.Context())
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
	shortKey, err := theRouter.getShortKey(request.Context(), urlToShort, userID)
	if err != nil && !errors.Is(err, ErrConflict) {
		logger.Log.Debugln("error while `theRouter.getShortKey()` calling: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
	shortURL := theRouter.getShortURL(shortKey)

	responseDTO := models.ShortenResponse{Result: shortURL}

	response.Header().Set("Content-Type", "application/json")

	resultStatus := http.StatusCreated
	if errors.Is(err, ErrConflict) {
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
	full, found, err := theRouter.db.FindFullByShort(req.Context(), short)
	if errors.Is(err, models.ErrURLMarkedAsDeleted) {
		res.WriteHeader(http.StatusGone)
		return
	}
	if err != nil {
		logger.Log.Debugln("error while `theRouter.db.FindFullByShort()` calling: ", zap.Error(err))
		res.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !found {
		res.WriteHeader(http.StatusNotFound)
		return
	}
	http.Redirect(res, req, full, http.StatusTemporaryRedirect)
}

// PostShorten handles plain text full URL.
// Responds with a plain text short URL or 409 on conflict.
func (theRouter Router) PostShorten(response http.ResponseWriter, request *http.Request) {
	urlToShort, err := getURLToShort(request)
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

	shortKey, err := theRouter.getShortKey(request.Context(), urlToShort, userID)
	if err != nil && !errors.Is(err, ErrConflict) {
		logger.Log.Debugln("error while `theRouter.getShortKey()` calling: ", zap.Error(err))
		http.Error(response, err.Error(), http.StatusInternalServerError)
		return
	}

	resultStatus := http.StatusCreated
	if errors.Is(err, ErrConflict) {
		resultStatus = http.StatusConflict
	}
	response.WriteHeader(resultStatus)

	_, err = response.Write([]byte(theRouter.getShortURL(shortKey)))
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
	numberOfShortenedURLs, err := theRouter.db.GetNumberOfShortenedURLs(request.Context())
	if err != nil {
		logger.Log.Debugln("Error calling the `theRouter.db.GetNumberOfShortenedURLs()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
	numberOfUsers, err := theRouter.db.GetNumberOfUsers(request.Context())
	if err != nil {
		logger.Log.Debugln("Error calling the `theRouter.db.GetNumberOfUsers()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
	responseDTO := models.InternalStatsResponse{
		URLs:  numberOfShortenedURLs,
		Users: numberOfUsers,
	}

	response.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(response).Encode(responseDTO)
	if err != nil {
		logger.Log.Debug("error encoding response", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func differenceStringSlices(a, b []string) []string {
	bSet := make(map[string]struct{}, len(b))
	for _, item := range b {
		bSet[item] = struct{}{}
	}

	var diff []string
	for _, item := range a {
		if _, found := bSet[item]; !found {
			diff = append(diff, item)
		}
	}
	return diff
}

func (theRouter Router) getValidator() *validator.Validate {
	if theRouter.validator == nil {
		theRouter.validator = validator.New()
	}
	return theRouter.validator
}

func (theRouter Router) fillThePostApishortenbatchResponse(
	response *models.BatchShortenResponse,
	fullsToShortsMap map[string]string,
	originalURLToCorrelationIDMap map[string]string,
) {
	for full, short := range fullsToShortsMap {
		*response = append(
			*response,
			models.BatchShortenResponseItem{
				CorrelationID: originalURLToCorrelationIDMap[full],
				ShortURL:      theRouter.getShortURL(short),
			},
		)
	}
}

func (theRouter Router) getPostApishortenbatchResponse(
	existentFullsToShortsMap map[string]string,
	unexistentFullsToShortsMap map[string]string,
	originalURLToCorrelationIDMap map[string]string,
) models.BatchShortenResponse {
	result := models.BatchShortenResponse{}
	theRouter.fillThePostApishortenbatchResponse(&result, existentFullsToShortsMap, originalURLToCorrelationIDMap)
	theRouter.fillThePostApishortenbatchResponse(&result, unexistentFullsToShortsMap, originalURLToCorrelationIDMap)

	return result
}

func (theRouter Router) getUnexistentFullsToShortsMap(unexistentFulls []string) map[string]string {
	result := map[string]string{}
	for _, full := range unexistentFulls {
		result[full] = uuid.New().String()
	}

	return result
}

func (theRouter Router) getOriginalURLToCorrelationIDMap(requestDTO models.BatchShortenRequest) map[string]string {
	result := map[string]string{}
	for _, originalURLToCorrelationID := range requestDTO {
		result[originalURLToCorrelationID.OriginalURL] = originalURLToCorrelationID.CorrelationID
	}

	return result
}

func (theRouter Router) getShortURL(shortKey string) string {
	return theRouter.shortURLBase + "/" + shortKey
}

func (theRouter Router) getShortKey(ctx context.Context, urlToShort string, userID string) (string, error) {
	transaction, err := theRouter.db.BeginTransaction()
	if err != nil {
		return "", err
	}

	short, found, err := theRouter.db.FindShortByFull(ctx, urlToShort, transaction)
	if err != nil {
		_ = theRouter.db.RollbackTransaction(transaction)

		return "", err
	}

	var result string
	var resultErr error

	if found {
		result = short
		resultErr = ErrConflict
	}

	if !found {
		short = uuid.New().String()
		err = theRouter.db.InsertURLMapping(ctx, short, urlToShort, transaction)
		if err != nil {
			_ = theRouter.db.RollbackTransaction(transaction)

			return "", err
		}
		result = short
		resultErr = nil
	}

	err = theRouter.db.SaveUserUrls(ctx, userID, []string{urlToShort}, transaction)
	if err != nil {
		_ = theRouter.db.RollbackTransaction(transaction)

		return "", err
	}

	err = theRouter.db.CommitTransaction(transaction)
	if err != nil {
		return "", err
	}

	return result, resultErr
}

func extractFirstURL(urlToShort string) (string, error) {
	match := urlPattern.FindString(urlToShort)
	if match == "" {
		return "", errors.New("there is no valid URL substring in the request body")
	}

	return match, nil
}

func getURLToShort(req *http.Request) (string, error) {
	urlToShort, err := io.ReadAll(req.Body)
	if err != nil {
		return "", err
	}

	urlToShortAsString := string(urlToShort)

	urlToShortAsString, err = extractFirstURL(urlToShortAsString)
	if err != nil {
		return "", err
	}

	return urlToShortAsString, nil
}
