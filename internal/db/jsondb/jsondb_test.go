package jsondb

import (
	"context"
	"github.com/patric-chuzhbe/urlshrt/internal/db/storage"
	"github.com/patric-chuzhbe/urlshrt/internal/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

const (
	testDBFileName = "db_test.json"
)

func Test(t *testing.T) {
	t.Run("The base jsondb package test", func(t *testing.T) {
		theStorage, err := New(testDBFileName)
		require.NoError(t, err)
		require.NotNil(t, theStorage)
		defer func() {
			err := theStorage.Close()
			require.NoError(t, err)
			err = os.Remove(testDBFileName)
			require.NoError(t, err)
		}()

		err = theStorage.InsertURLMapping(context.Background(), "some short", "some full", nil)
		assert.NoError(t, err, "The `theStorage.Insert()` should not return error")

		short, found, err := theStorage.FindShortByFull(context.Background(), "some full", nil)
		assert.NoError(t, err, "The `theStorage.Insert()` should not return error")
		assert.True(t, found)
		assert.Equal(t, "some short", short, "Should be equal to `some short`")

		shorts, err := theStorage.FindShortsByFulls(
			context.Background(),
			[]string{"some full", "some unexistent full"},
			nil,
		)
		assert.NoError(t, err, "The `theStorage.FindShortsByFulls()` should not return error")
		assert.Equal(
			t,
			map[string]string{"some full": "some short"},
			shorts,
			"Should be equal to map[string]string{\"some full\": \"some short\"}",
		)

		err = theStorage.SaveNewFullsAndShorts(
			context.Background(),
			map[string]string{
				"one":   "1-1-1",
				"two":   "2-2-2",
				"three": "3-3-3",
			},
			nil,
		)
		assert.NoError(t, err, "The `theStorage.SaveNewFullsAndShorts()` should not return error")

		shortsByFulls, err := theStorage.FindShortsByFulls(
			context.Background(),
			[]string{
				"one",
				"two",
				"three",
			},
			nil,
		)
		assert.NoError(t, err, "The `theStorage.FindShortsByFulls()` should not return error")
		assert.Equal(
			t,
			map[string]string{
				"one":   "1-1-1",
				"two":   "2-2-2",
				"three": "3-3-3",
			},
			shortsByFulls,
			"the `theStorage.FindShortsByFulls()`'s result should be equal to the target value",
		)

		err = theStorage.Ping(context.Background())
		assert.NoError(t, err, "The jsondb.Ping() should not return error")

		err = theStorage.Close()
		assert.NoError(t, err, "The jsondb.Close() should not return error")

		userID, err := theStorage.CreateUser(context.Background(), &user.User{}, nil)
		assert.NoError(t, err)
		assert.Equal(t, 1, userID)

		usr, err := theStorage.GetUserByID(context.Background(), 1, nil)
		assert.NoError(t, err)
		assert.Equal(t, &user.User{ID: 1}, usr)

		usr, err = theStorage.GetUserByID(context.Background(), 10, nil)
		assert.NoError(t, err)
		assert.Equal(t, &user.User{ID: 0}, usr)

		userID2, err := theStorage.CreateUser(context.Background(), &user.User{}, nil)
		assert.NoError(t, err)
		assert.Equal(t, 2, userID2)

		err = theStorage.SaveUserUrls(
			context.Background(),
			userID,
			[]string{
				"one",
				"two",
			},
			nil,
		)
		assert.NoError(t, err)
		err = theStorage.SaveUserUrls(
			context.Background(),
			userID2,
			[]string{
				"three",
				"some full",
			},
			nil,
		)
		assert.NoError(t, err)

		err = theStorage.RemoveUsersUrls(
			context.Background(),
			map[int][]string{
				1: {
					"1-1-1",
					"2-2-2",
					"3-3-3",
					"some short",
				},
				2: {
					"1-1-1",
					"2-2-2",
					"3-3-3",
					"some short",
				},
			},
		)
		assert.NoError(t, err)

		for _, short := range []string{
			"1-1-1",
			"2-2-2",
			"3-3-3",
			"some short",
		} {
			_, _, err = theStorage.FindFullByShort(context.Background(), short)
			assert.ErrorIs(t, err, storage.ErrURLMarkedAsDeleted)
		}
	})
}
