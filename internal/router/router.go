package router

import (
	"encoding/json"
	"errors"
	chi "github.com/go-chi/chi/v5"
	validator "github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	config "github.com/patric-chuzhbe/urlshrt/internal/config"
	db "github.com/patric-chuzhbe/urlshrt/internal/db"
	gzippedHttp "github.com/patric-chuzhbe/urlshrt/internal/gzippedhttp"
	logger "github.com/patric-chuzhbe/urlshrt/internal/logger"
	models "github.com/patric-chuzhbe/urlshrt/internal/models"
	"go.uber.org/zap"
	"io"
	"net/http"
	"regexp"
)

var theDB *db.SimpleJSONDB

var urlPattern = regexp.MustCompile(`\bhttps?://\S+\b`)

func getShortURL(shortKey string) string {
	return config.Values.ShortURLBase + "/" + shortKey
}

func PostApishorten(response http.ResponseWriter, request *http.Request) {
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
	shortKey, err := getShortKey(urlToShort)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
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

func GetRedirecttofullurl(res http.ResponseWriter, req *http.Request) {
	short := chi.URLParam(req, "short")
	full, found := theDB.FindFullByShort(short)
	if !found {
		res.WriteHeader(http.StatusNotFound)
		return
	}
	http.Redirect(res, req, full, http.StatusTemporaryRedirect)
}

func getShortKey(urlToShort string) (string, error) {
	short, found := theDB.FindShortByFull(urlToShort)
	if found {
		return short, nil
	}
	short = uuid.New().String()
	theDB.Insert(short, urlToShort)

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

func PostShorten(res http.ResponseWriter, req *http.Request) {
	urlToShort, err := getURLToShort(req)
	if err != nil {
		http.Error(res, err.Error(), http.StatusBadRequest)
		return
	}

	shortKey, err := getShortKey(urlToShort)
	if err != nil {
		http.Error(res, err.Error(), http.StatusBadRequest)
		return
	}

	res.WriteHeader(http.StatusCreated)

	_, err = res.Write([]byte(getShortURL(shortKey)))
	if err != nil {
		http.Error(res, err.Error(), http.StatusBadRequest)
		return
	}
}

func New(database *db.SimpleJSONDB) *chi.Mux {
	theDB = database
	router := chi.NewRouter()
	router.Use(
		logger.WithLoggingHTTPMiddleware,
		gzippedHttp.UngzipJSONAndTextHTMLRequest,
	)
	router.With(gzippedHttp.GzipResponse).Post(`/`, PostShorten)
	router.Get(`/{short}`, GetRedirecttofullurl)
	router.With(gzippedHttp.GzipResponse).Post(`/api/shorten`, PostApishorten)

	return router
}
