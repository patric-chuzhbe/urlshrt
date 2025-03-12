package router

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	gzippedHttp "github.com/patric-chuzhbe/urlshrt/internal/gzippedhttp"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/thoas/go-funk"
	"go.uber.org/zap"
	"io"
	"net/http"
	"regexp"
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
		shortURLFormatter func(string) string,
	) (models.UserUrls, error)

	SaveUserUrls(
		ctx context.Context,
		userID string,
		urls []string,
		transaction *sql.Tx,
	) error
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

type router struct {
	db           storage
	shortURLBase string
	urlsRemover  urlsRemover
}

var urlPattern = regexp.MustCompile(`\bhttps?://\S+\b`)

var ErrConflict = errors.New("data conflict")

func (theRouter router) DeleteApiuserurls(response http.ResponseWriter, request *http.Request) {
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

func (theRouter router) GetApiuserurls(response http.ResponseWriter, request *http.Request) {
	userID, ok := request.Context().Value(auth.UserIDKey).(string)
	if !ok || userID == "" {
		response.WriteHeader(http.StatusUnauthorized)

		return
	}
	responseDTO, err := theRouter.db.GetUserUrls(context.Background(), userID, theRouter.getShortURL)
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

func (theRouter router) fillThePostApishortenbatchResponse(
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

func (theRouter router) getPostApishortenbatchResponse(
	existentFullsToShortsMap map[string]string,
	unexistentFullsToShortsMap map[string]string,
	originalURLToCorrelationIDMap map[string]string,
) models.BatchShortenResponse {
	result := models.BatchShortenResponse{}
	theRouter.fillThePostApishortenbatchResponse(&result, existentFullsToShortsMap, originalURLToCorrelationIDMap)
	theRouter.fillThePostApishortenbatchResponse(&result, unexistentFullsToShortsMap, originalURLToCorrelationIDMap)

	return result
}

func (theRouter router) getUnexistentFullsToShortsMap(unexistentFulls []string) map[string]string {
	result := map[string]string{}
	for _, full := range unexistentFulls {
		result[full] = uuid.New().String()
	}

	return result
}

func (theRouter router) getOriginalURLToCorrelationIDMap(requestDTO models.BatchShortenRequest) map[string]string {
	result := map[string]string{}
	for _, originalURLToCorrelationID := range requestDTO {
		result[originalURLToCorrelationID.OriginalURL] = originalURLToCorrelationID.CorrelationID
	}

	return result
}

func (theRouter router) PostApishortenbatch(response http.ResponseWriter, request *http.Request) {
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

	existentFullsToShortsMap, err := theRouter.db.FindShortsByFulls(context.Background(), originalUrls, transaction)
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
	unexistentFullsAsInterface, _ := funk.Difference(originalUrls, existentFulls)
	unexistentFulls := unexistentFullsAsInterface.([]string)
	unexistentFullsToShortsMap := theRouter.getUnexistentFullsToShortsMap(unexistentFulls)
	err = theRouter.db.SaveNewFullsAndShorts(context.Background(), unexistentFullsToShortsMap, transaction)
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
		context.Background(),
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

func (theRouter router) getShortURL(shortKey string) string {
	return theRouter.shortURLBase + "/" + shortKey
}

func (theRouter router) GetPing(response http.ResponseWriter, request *http.Request) {
	err := theRouter.db.Ping(context.Background())
	if err != nil {
		response.WriteHeader(http.StatusInternalServerError)

		return
	}
	response.WriteHeader(http.StatusOK)
}

func (theRouter router) PostApishorten(response http.ResponseWriter, request *http.Request) {
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
	shortKey, err := theRouter.getShortKey(urlToShort, userID)
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

func (theRouter router) GetRedirecttofullurl(res http.ResponseWriter, req *http.Request) {
	short := chi.URLParam(req, "short")
	full, found, err := theRouter.db.FindFullByShort(context.Background(), short)
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

func (theRouter router) getShortKey(urlToShort string, userID string) (string, error) {
	transaction, err := theRouter.db.BeginTransaction()
	if err != nil {
		return "", err
	}

	short, found, err := theRouter.db.FindShortByFull(context.Background(), urlToShort, transaction)
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
		err = theRouter.db.InsertURLMapping(context.Background(), short, urlToShort, transaction)
		if err != nil {
			_ = theRouter.db.RollbackTransaction(transaction)

			return "", err
		}
		result = short
		resultErr = nil
	}

	err = theRouter.db.SaveUserUrls(context.Background(), userID, []string{urlToShort}, transaction)
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

func (theRouter router) PostShorten(response http.ResponseWriter, request *http.Request) {
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

	shortKey, err := theRouter.getShortKey(urlToShort, userID)
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

func New(
	database storage,
	shortURLBase string,
	auth authenticator,
	urlsRemover urlsRemover,
) *chi.Mux {
	myRouter := router{
		db:           database,
		shortURLBase: shortURLBase,
		urlsRemover:  urlsRemover,
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

	return router
}
