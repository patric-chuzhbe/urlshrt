package router

import (
	"context"
	"encoding/json"
	"errors"
	chi "github.com/go-chi/chi/v5"
	validator "github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/patric-chuzhbe/urlshrt/internal/auth"
	"github.com/patric-chuzhbe/urlshrt/internal/authenticator"
	"github.com/patric-chuzhbe/urlshrt/internal/db/storage"
	gzippedHttp "github.com/patric-chuzhbe/urlshrt/internal/gzippedhttp"
	logger "github.com/patric-chuzhbe/urlshrt/internal/logger"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/thoas/go-funk"
	"go.uber.org/zap"
	"io"
	"net/http"
	"regexp"
)

type router struct {
	db           storage.Storage
	shortURLBase string
}

var urlPattern = regexp.MustCompile(`\bhttps?://\S+\b`)

var ErrConflict = errors.New("data conflict")

func (theRouter router) GetApiuserurls(response http.ResponseWriter, request *http.Request) {
	userID, ok := request.Context().Value(auth.UserIDKey).(int)
	if !ok || userID == 0 {
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

	userID, ok := request.Context().Value(auth.UserIDKey).(int)
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

	userID, ok := request.Context().Value(auth.UserIDKey).(int)
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

func (theRouter router) getShortKey(urlToShort string, userID int) (string, error) {
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

	userID, ok := request.Context().Value(auth.UserIDKey).(int)
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

func New(database storage.Storage, shortURLBase string, auth authenticator.Authenticator) *chi.Mux {
	myRouter := router{
		db:           database,
		shortURLBase: shortURLBase,
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

	return router
}
