package router

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/patric-chuzhbe/urlshrt/internal/config"
	"github.com/patric-chuzhbe/urlshrt/internal/db"
	"github.com/patric-chuzhbe/urlshrt/internal/logger"
	"io"
	"net/http"
	"regexp"
)

var theDB *db.SimpleJSONDB

var urlPattern = regexp.MustCompile(`\bhttps?://\S+\b`)

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

	_, err = res.Write([]byte(config.Values.ShortURLBase + "/" + shortKey))
	if err != nil {
		http.Error(res, err.Error(), http.StatusBadRequest)
		return
	}
}

func New(database *db.SimpleJSONDB) *chi.Mux {
	theDB = database
	router := chi.NewRouter()
	router.Use(logger.WithLoggingHTTPMiddleware)
	router.Post(`/`, PostShorten)
	router.Get(`/{short}`, GetRedirecttofullurl)

	return router
}
