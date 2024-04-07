package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
)

const (
	TRIES_TO_GENERATE_UNIQUE_KEY = 10
	AMT_OF_SYMBOLS_TO_GENERATE   = 8
	DB_FILE_NAME                 = "db.json"
	SHORT_URL_TEMPLATE           = "http://localhost:8080/%s"
)

type SimpleJsonDb struct {
	fileName string
	//Cache    map[string]string

	cache CacheStruct
}

type CacheStruct struct {
	ShortToFull map[string]string
	FullToShort map[string]string
}

var theDb *SimpleJsonDb

func initDbFile(fileName string) error {
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

func writeToJsonFile(fileName string, cache interface{}) error {
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

func parseJsonFile(fileName string, cacheMap *CacheStruct) error {
	file, err := os.Open(fileName)
	if err != nil {
		return err /*errors.New(fmt.Sprintf("error opening file: %s", err.Error()))*/
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(cacheMap)
	if err != nil {
		return err /*errors.New(fmt.Sprintf("error decoding JSON: %s", err.Error()))*/
	}

	return nil
}

func NewSimpleJsonDb(fileName string) (*SimpleJsonDb, error) {
	simpleJsonDb := SimpleJsonDb{
		fileName: fileName,
		cache:    CacheStruct{},
	}

	err := parseJsonFile(simpleJsonDb.fileName, &simpleJsonDb.cache)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		err := initDbFile(fileName)
		if err != nil {
			return nil, err
		}
		err = parseJsonFile(simpleJsonDb.fileName, &simpleJsonDb.cache)
		if err != nil {
			return nil, err
		}
	}

	return &simpleJsonDb, nil
}

func (db *SimpleJsonDb) Insert(short, full string) {
	db.cache.ShortToFull[short] = full
	db.cache.FullToShort[full] = short
}

func (db *SimpleJsonDb) Close() error {
	err := writeToJsonFile(db.fileName, db.cache)
	if err != nil {
		return err
	}

	return nil
}

func (db *SimpleJsonDb) FindFullByShort(short string) (full string, found bool) {
	full, found = db.cache.ShortToFull[short]
	return /*db.cache.ShortToFull[short]*/
}

func (db *SimpleJsonDb) FindShortByFull(full string) (short string, found bool) {
	short, found = db.cache.FullToShort[full]
	return /*db.cache.FullToShort[full]*/
}

func (db *SimpleJsonDb) IsShortExists(short string) bool {
	_, exists := db.cache.ShortToFull[short]
	return exists
}

func redirectToFullUrl(res http.ResponseWriter, req *http.Request) {
	short := mux.Vars(req)["short"]
	full, found := theDb.FindFullByShort(short)
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
	for i := 0; i < TRIES_TO_GENERATE_UNIQUE_KEY; i++ {
		shortKey = generateRandomString(AMT_OF_SYMBOLS_TO_GENERATE)
		if !theDb.IsShortExists(shortKey) {
			return shortKey, nil
		}
	}
	return "", errors.New("the number of attempts to generate a unique key has been exceeded")
}

func getShortKey(urlToShort string) (string, error) {
	short, found := theDb.FindShortByFull(urlToShort)
	if found {
		return short, nil
	}
	short, err := generateShortKey()
	if err != nil {
		return "", err
	}
	theDb.Insert(short, urlToShort)

	return short, nil
}

func extractFirstUrl(urlToShort string) (string, error) {
	urlPattern := regexp.MustCompile(`\bhttps?://\S+\b`)
	match := urlPattern.FindString(urlToShort)
	if match == "" {
		return "", errors.New("there is no valid URL substring in the request body")
	}

	return match, nil
}

func getUrlToShort(req *http.Request) (string, error) {
	//urlToShort := make([]byte, req.ContentLength)
	//bytesRead, err := req.Body.Read(urlToShort)
	urlToShort, err := io.ReadAll(req.Body)
	if err != nil {
		return "", err
	}

	urlToShortAsString := string(urlToShort)

	urlToShortAsString, err = extractFirstUrl(urlToShortAsString)
	if err != nil {
		return "", err
	}

	return urlToShortAsString, nil
}

func mainPage(res http.ResponseWriter, req *http.Request) {
	urlToShort, err := getUrlToShort(req)
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

	_, err = res.Write([]byte(fmt.Sprintf(SHORT_URL_TEMPLATE, shortKey)))
	if err != nil {
		http.Error(res, err.Error(), http.StatusBadRequest)
		return
	}
}

func main() {
	var err error
	theDb, err = NewSimpleJsonDb(DB_FILE_NAME)
	if err != nil {
		panic(err)
	}
	defer func() {
		err := theDb.Close()
		if err != nil {
			panic(err)
		}
	}()

	router := mux.NewRouter()
	router.HandleFunc(`/`, mainPage)
	router.HandleFunc(`/{short}`, redirectToFullUrl)

	fmt.Println("listening port 8080...")

	// Handle SIGINT signal (Ctrl+C)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nReceived interrupt signal, closing database and exiting...")
		err := theDb.Close()
		if err != nil {
			panic(err)
		}
		os.Exit(0)
	}()

	err = http.ListenAndServe(`:8080`, router /*mux*/)
	if err != nil {
		panic(err)
	}
}
