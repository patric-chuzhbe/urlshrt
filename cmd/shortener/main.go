package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/patric-chuzhbe/urlshrt/internal/config"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
)

const (
	TriesToGenerateUniqueKey = 10
	AmtOfSymbolsToGenerate   = 8
	DBFileName               = "db.json"
	//ShortURLTemplate         = "http://localhost:8080/%s"
)

type SimpleJSONDB struct {
	fileName string
	cache    CacheStruct
}

type CacheStruct struct {
	ShortToFull map[string]string
	FullToShort map[string]string
}

var theDB *SimpleJSONDB

func initDBFile(fileName string) error {
	dbFile, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(dbFile, `{
	"ShortToFull": {},
	"FullToShort": {}
}`)
	if err != nil {
		return err
	}
	return dbFile.Close()
}

func writeToJSONFile(fileName string, cache interface{}) error {
	jsonData, err := json.MarshalIndent(cache, "", "\t")
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %s", err)
	}

	file, err2 := os.OpenFile(fileName, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err2 != nil {
		return fmt.Errorf("error opening file: %s", err2)
	}
	defer file.Close()

	_, err = file.Write(jsonData)
	if err != nil {
		return fmt.Errorf("error writing to file: %s", err)
	}

	return nil
}

func parseJSONFile(fileName string, cacheMap *CacheStruct) error {
	file, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(cacheMap)
	if err != nil {
		return err
	}

	return nil
}

func NewSimpleJSONDB(fileName string) (*SimpleJSONDB, error) {
	simpleJSONDB := SimpleJSONDB{
		fileName: fileName,
		cache:    CacheStruct{},
	}

	err := parseJSONFile(simpleJSONDB.fileName, &simpleJSONDB.cache)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		err := initDBFile(fileName)
		if err != nil {
			return nil, err
		}
		err = parseJSONFile(simpleJSONDB.fileName, &simpleJSONDB.cache)
		if err != nil {
			return nil, err
		}
	}

	return &simpleJSONDB, nil
}

func (db *SimpleJSONDB) Insert(short, full string) {
	db.cache.ShortToFull[short] = full
	db.cache.FullToShort[full] = short
}

func (db *SimpleJSONDB) Close() error {
	err := writeToJSONFile(db.fileName, db.cache)
	if err != nil {
		return err
	}

	return nil
}

func (db *SimpleJSONDB) FindFullByShort(short string) (full string, found bool) {
	full, found = db.cache.ShortToFull[short]
	return
}

func (db *SimpleJSONDB) FindShortByFull(full string) (short string, found bool) {
	short, found = db.cache.FullToShort[full]
	return
}

func (db *SimpleJSONDB) IsShortExists(short string) bool {
	_, exists := db.cache.ShortToFull[short]
	return exists
}

func redirectToFullURL(res http.ResponseWriter, req *http.Request) {
	short := chi.URLParam(req, "short")
	full, found := theDB.FindFullByShort(short)
	if !found {
		res.WriteHeader(http.StatusNotFound)
		_, err := res.Write([]byte("404 Not Found"))
		if err != nil {
			http.Error(res, err.Error(), http.StatusBadRequest)
			return
		}
		return
	}
	http.Redirect(res, req, full, http.StatusTemporaryRedirect)
}

func generateRandomString(length int) string {
	const symbols = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var result string

	for i := 0; i < length; i++ {
		randomIndex, _ := rand.Int(rand.Reader, big.NewInt(int64(len(symbols))))
		result += string(symbols[randomIndex.Int64()])
	}

	return result
}

func generateShortKey() (string, error) {
	shortKey := ""
	for i := 0; i < TriesToGenerateUniqueKey; i++ {
		shortKey = generateRandomString(AmtOfSymbolsToGenerate)
		if !theDB.IsShortExists(shortKey) {
			return shortKey, nil
		}
	}
	return "", errors.New("the number of attempts to generate a unique key has been exceeded")
}

func getShortKey(urlToShort string) (string, error) {
	short, found := theDB.FindShortByFull(urlToShort)
	if found {
		return short, nil
	}
	short, err := generateShortKey()
	if err != nil {
		return "", err
	}
	theDB.Insert(short, urlToShort)

	return short, nil
}

func extractFirstURL(urlToShort string) (string, error) {
	urlPattern := regexp.MustCompile(`\bhttps?://\S+\b`)
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

func mainPage(res http.ResponseWriter, req *http.Request) {
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

func main() {
	config.Init()

	var err error

	theDB, err = NewSimpleJSONDB(DBFileName)
	if err != nil {
		panic(err)
	}
	defer func() {
		err := theDB.Close()
		if err != nil {
			panic(err)
		}
	}()

	router := chi.NewRouter()
	router.Post(`/`, mainPage)
	router.Get(`/{short}`, redirectToFullURL)

	// Handle SIGINT signal (Ctrl+C)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nReceived interrupt signal, closing database and exiting...")
		err := theDB.Close()
		if err != nil {
			panic(err)
		}
		os.Exit(0)
	}()

	fmt.Println("Running server on", config.Values.RunAddr)

	err = http.ListenAndServe(config.Values.RunAddr, router)
	if err != nil {
		panic(err)
	}
}
