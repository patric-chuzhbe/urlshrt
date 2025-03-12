package jsondb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/patric-chuzhbe/urlshrt/internal/models"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
	"github.com/thoas/go-funk"
	"os"
)

type JSONDB struct {
	fileName string
	Cache    CacheStruct
}

type CacheStruct struct {
	ShortToFull        map[string]string
	FullToShort        map[string]string
	Users              map[string]*user.User
	UsersIdsToUrlsMap  map[string][]string
	UrlsToUsersIdsMap  map[string][]string
	UrlsToIsDeletedMap map[string]bool
}

func (db *JSONDB) RemoveUsersUrls(
	ctx context.Context,
	usersURLs map[string][]string,
) error {
	for userID, shortURLs := range usersURLs {
		for _, shortURL := range shortURLs {
			fullURL := db.Cache.ShortToFull[shortURL]
			usersIds, ok := db.Cache.UrlsToUsersIdsMap[fullURL]
			if ok && funk.Contains(usersIds, userID) {
				db.Cache.UrlsToIsDeletedMap[fullURL] = true
			}
		}
	}

	return nil
}

func (db *JSONDB) SaveUserUrls(
	ctx context.Context,
	userID string,
	urls []string,
	transaction *sql.Tx,
) error {
	for _, url := range urls {
		_, exists := db.Cache.UsersIdsToUrlsMap[userID]
		if !exists {
			db.Cache.UsersIdsToUrlsMap[userID] = []string{}
		}
		db.Cache.UsersIdsToUrlsMap[userID] = append(db.Cache.UsersIdsToUrlsMap[userID], url)

		_, exists = db.Cache.UrlsToUsersIdsMap[url]
		if !exists {
			db.Cache.UrlsToUsersIdsMap[url] = []string{}
		}
		db.Cache.UrlsToUsersIdsMap[url] = append(db.Cache.UrlsToUsersIdsMap[url], userID)
	}

	return nil
}

func (db *JSONDB) GetUserUrls(
	ctx context.Context,
	userID string,
	shortURLFormatter func(string) string,
) (models.UserUrls, error) {
	formatter := func(str string) string { return str }
	if shortURLFormatter != nil {
		formatter = shortURLFormatter
	}

	result := models.UserUrls{}
	urls, exists := db.Cache.UsersIdsToUrlsMap[userID]
	if exists {
		for _, url := range urls {
			result = append(
				result,
				models.UserURL{
					ShortURL:    formatter(db.Cache.FullToShort[url]),
					OriginalURL: url,
				},
			)
		}
	}

	return result, nil
}

func (db *JSONDB) CreateUser(ctx context.Context, usr *user.User, transaction *sql.Tx) (string, error) {
	usr.ID = uuid.New().String()
	db.Cache.Users[usr.ID] = usr
	return usr.ID, nil
}

func (db *JSONDB) GetUserByID(ctx context.Context, userID string, transaction *sql.Tx) (*user.User, error) {
	usr, found := db.Cache.Users[userID]
	if found {
		return usr, nil
	}

	return &user.User{ID: ""}, nil
}

func (db *JSONDB) CommitTransaction(transaction *sql.Tx) error {
	return nil
}

func (db *JSONDB) RollbackTransaction(transaction *sql.Tx) error {
	return nil
}

func (db *JSONDB) BeginTransaction() (*sql.Tx, error) {
	return nil, nil
}

func (db *JSONDB) SaveNewFullsAndShorts(
	ctx context.Context,
	unexistentFullsToShortsMap map[string]string,
	transaction *sql.Tx,
) error {
	for full, short := range unexistentFullsToShortsMap {
		err := db.InsertURLMapping(ctx, short, full, transaction)
		if err != nil {
			return err
		}
	}

	return nil
}

func (db *JSONDB) FindShortsByFulls(
	ctx context.Context,
	originalUrls []string,
	transaction *sql.Tx,
) (map[string]string, error) {
	result := map[string]string{}
	for _, full := range originalUrls {
		short, found, err := db.FindShortByFull(ctx, full, transaction)
		if err != nil {
			return nil, err
		}
		if found {
			result[full] = short
		}
	}

	return result, nil
}

func initDBFile(fileName string) error {
	dbFile, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(dbFile, `{
	"ShortToFull": {},
	"FullToShort": {},
	"Users": {},
	"UsersIdsToUrlsMap": {},
	"UrlsToUsersIdsMap": {},
	"UrlsToIsDeletedMap": {}
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

func New(fileName string) (*JSONDB, error) {
	simpleJSONDB := JSONDB{
		fileName: fileName,
		Cache:    CacheStruct{},
	}

	err := parseJSONFile(simpleJSONDB.fileName, &simpleJSONDB.Cache)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		err := initDBFile(fileName)
		if err != nil {
			return nil, err
		}
		err = parseJSONFile(simpleJSONDB.fileName, &simpleJSONDB.Cache)
		if err != nil {
			return nil, err
		}
	}

	return &simpleJSONDB, nil
}

func (db *JSONDB) Ping(ctx context.Context) error {
	return nil
}

func (db *JSONDB) InsertURLMapping(
	ctx context.Context,
	short string,
	full string,
	transaction *sql.Tx,
) error {
	db.Cache.ShortToFull[short] = full
	db.Cache.FullToShort[full] = short

	return nil
}

func (db *JSONDB) Close() error {
	err := writeToJSONFile(db.fileName, db.Cache)
	if err != nil {
		return err
	}

	return nil
}

func (db *JSONDB) FindFullByShort(ctx context.Context, short string) (full string, found bool, err error) {
	full, found = db.Cache.ShortToFull[short]
	err = nil

	isDeleted, ok := db.Cache.UrlsToIsDeletedMap[full]
	if ok && isDeleted {
		err = models.ErrURLMarkedAsDeleted
	}

	return
}

func (db *JSONDB) FindShortByFull(
	ctx context.Context,
	full string,
	transaction *sql.Tx,
) (short string, found bool, err error) {
	short, found = db.Cache.FullToShort[full]
	err = nil

	return
}

func (db *JSONDB) IsShortExists(ctx context.Context, short string) (bool, error) {
	_, exists := db.Cache.ShortToFull[short]

	return exists, nil
}
