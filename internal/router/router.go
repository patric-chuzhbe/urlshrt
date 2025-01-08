package router

import (
	"context"
	"encoding/json"
	"errors"
	chi "github.com/go-chi/chi/v5"
	validator "github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	config "github.com/patric-chuzhbe/urlshrt/internal/config"
	gzippedHttp "github.com/patric-chuzhbe/urlshrt/internal/gzippedhttp"
	logger "github.com/patric-chuzhbe/urlshrt/internal/logger"
	models "github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/storage"
	"github.com/thoas/go-funk"
	"go.uber.org/zap"
	"io"
	"net/http"
	"regexp"
)

type router struct {
	theDB storage.Storage
}

var urlPattern = regexp.MustCompile(`\bhttps?://\S+\b`)

func fillThePostApishortenbatchResponse(
	response *models.PostApishortenbatchResponse,
	fullsToShortsMap map[string]string,
	originalURLToCorrelationIDMap map[string]string,
) {
	for full, short := range fullsToShortsMap {
		*response = append(
			*response,
			models.ShortURLToCorrelationID{
				CorrelationID: originalURLToCorrelationIDMap[full],
				ShortURL:      getShortURL(short),
			},
		)
	}
}

func (theRouter router) getPostApishortenbatchResponse(
	existentFullsToShortsMap map[string]string,
	unexistentFullsToShortsMap map[string]string,
	originalURLToCorrelationIDMap map[string]string,
) models.PostApishortenbatchResponse {
	result := models.PostApishortenbatchResponse{}
	fillThePostApishortenbatchResponse(&result, existentFullsToShortsMap, originalURLToCorrelationIDMap)
	fillThePostApishortenbatchResponse(&result, unexistentFullsToShortsMap, originalURLToCorrelationIDMap)

	return result
}

func (theRouter router) getUnexistentFullsToShortsMap(unexistentFulls []string) map[string]string {
	result := map[string]string{}
	for _, full := range unexistentFulls {
		result[full] = uuid.New().String()
	}

	return result
}

func (theRouter router) getOriginalURLToCorrelationIDMap(requestDTO models.PostApishortenbatchRequest) map[string]string {
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

	var requestDTO models.PostApishortenbatchRequest
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

	transaction, err := theRouter.theDB.BeginTransaction()
	if err != nil {
		logger.Log.Debugln("Error calling the `theRouter.theDB.BeginTransaction()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	originalURLToCorrelationIDMap := theRouter.getOriginalURLToCorrelationIDMap(requestDTO)

	originalUrls := funk.Keys(originalURLToCorrelationIDMap).([]string)

	existentFullsToShortsMap, err := theRouter.theDB.FindShortsByFulls(context.Background(), originalUrls, transaction)
	if err != nil {
		logger.Log.Debugln("Error calling the `theRouter.theDB.FindShortsByFulls()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	unexistentFullsAsInterface, _ := funk.Difference(
		originalUrls,
		funk.Keys(existentFullsToShortsMap).([]string),
	)
	unexistentFulls := unexistentFullsAsInterface.([]string)

	unexistentFullsToShortsMap := theRouter.getUnexistentFullsToShortsMap(unexistentFulls)

	err = theRouter.theDB.SaveNewFullsAndShorts(context.Background(), unexistentFullsToShortsMap, transaction)
	if err != nil {
		logger.Log.Debugln("Error calling the `theRouter.theDB.SaveNewFullsAndShorts()`: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}

	responseDTO := theRouter.getPostApishortenbatchResponse(
		existentFullsToShortsMap,
		unexistentFullsToShortsMap,
		originalURLToCorrelationIDMap,
	)

	err = transaction.Commit()
	if err != nil {
		logger.Log.Debugln("Error calling the `transaction.Commit()`: ", zap.Error(err))
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

func getShortURL(shortKey string) string {
	return config.Values.ShortURLBase + "/" + shortKey
}

func (theRouter router) GetPing(response http.ResponseWriter, request *http.Request) {
	err := theRouter.theDB.Ping(context.Background())
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

	var requestDTO models.Request
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

	urlToShort := requestDTO.URL
	shortKey, err := theRouter.getShortKey(urlToShort)
	if err != nil {
		logger.Log.Debugln("error while `theRouter.getShortKey()` calling: ", zap.Error(err))
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
	shortURL := getShortURL(shortKey)

	responseDTO := models.Response{Result: shortURL}

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(response).Encode(responseDTO); err != nil {
		logger.Log.Debug("error encoding response", zap.Error(err))
		return
	}
}

func (theRouter router) GetRedirecttofullurl(res http.ResponseWriter, req *http.Request) {
	short := chi.URLParam(req, "short")
	full, found, err := theRouter.theDB.FindFullByShort(context.Background(), short)
	if err != nil {
		logger.Log.Debugln("error while `theRouter.theDB.FindFullByShort()` calling: ", zap.Error(err))
		res.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !found {
		res.WriteHeader(http.StatusNotFound)
		return
	}
	http.Redirect(res, req, full, http.StatusTemporaryRedirect)
}

func (theRouter router) getShortKey(urlToShort string) (string, error) {
	short, found, err := theRouter.theDB.FindShortByFull(context.Background(), urlToShort)
	if err != nil {
		return "", err
	}
	if found {
		return short, nil
	}
	short = uuid.New().String()
	err = theRouter.theDB.Insert(context.Background(), short, urlToShort)
	if err != nil {
		return "", err
	}

	return short, nil
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

func (theRouter router) PostShorten(res http.ResponseWriter, req *http.Request) {
	urlToShort, err := getURLToShort(req)
	if err != nil {
		http.Error(res, err.Error(), http.StatusBadRequest)
		return
	}

	shortKey, err := theRouter.getShortKey(urlToShort)
	if err != nil {
		logger.Log.Debugln("error while `theRouter.getShortKey()` calling: ", zap.Error(err))
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}

	res.WriteHeader(http.StatusCreated)

	_, err = res.Write([]byte(getShortURL(shortKey)))
	if err != nil {
		http.Error(res, err.Error(), http.StatusBadRequest)
		return
	}
}

func New(database storage.Storage) *chi.Mux {
	myRouter := router{
		theDB: database,
	}
	router := chi.NewRouter()
	router.Use(
		logger.WithLoggingHTTPMiddleware,
		gzippedHttp.UngzipJSONAndTextHTMLRequest,
	)
	router.With(gzippedHttp.GzipResponse).Post(`/`, myRouter.PostShorten)
	router.Get(`/{short}`, myRouter.GetRedirecttofullurl)
	router.With(gzippedHttp.GzipResponse).Post(`/api/shorten`, myRouter.PostApishorten)
	router.Get(`/ping`, myRouter.GetPing)
	router.With(gzippedHttp.GzipResponse).Post(`/api/shorten/batch`, myRouter.PostApishortenbatch)

	return router
}
