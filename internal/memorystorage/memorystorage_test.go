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

		err = theStorage.Insert(context.Background(), "some short", "some full")
		assert.NoError(t, err, "The `theStorage.Insert()` should not return error")

		short, found, err := theStorage.FindShortByFull(context.Background(), "some full")
		assert.NoError(t, err, "The `theStorage.Insert()` should not return error")
		assert.True(t, found)
		assert.Equal(t, "some short", short, "Should be equal to `some short`")

		err = theStorage.Ping(context.Background())
		assert.NoError(t, err, "The memorystorage.Ping() should not return error")

		err = theStorage.Close()
		assert.NoError(t, err, "The memorystorage.Close() should not return error")
	})
}
