package memorystorage

import (
	"context"
	"github.com/stretchr/testify/assert"
	"testing"
)

func Test(t *testing.T) {
	t.Run("The base memorystorage package test", func(t *testing.T) {
		theStorage, err := New()
		assert.NoError(t, err, "The memorystorage.New() should not return error")

		err = theStorage.Insert(context.Background(), "some short", "some full", nil)
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
		assert.NoError(t, err, "The memorystorage.Ping() should not return error")

		err = theStorage.Close()
		assert.NoError(t, err, "The memorystorage.Close() should not return error")
	})
}
