package simplejsondb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

type SimpleJSONDB struct {
	fileName string
	cache    CacheStruct
}

type CacheStruct struct {
	ShortToFull map[string]string
	FullToShort map[string]string
}

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

func New(fileName string) (*SimpleJSONDB, error) {
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

func (db *SimpleJSONDB) Ping(outerCtx context.Context) error {
	return nil
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
