package config

import (
	"testing"

	"github.com/caarlos0/env/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClarifyShortURLBaseDisableHttps(t *testing.T) {
	values := Config{}

	err := env.Parse(&values)
	require.NoError(t, err)

	err = values.clarifyShortURLBase()
	require.NoError(t, err)

	assert.Equal(t, values.ShortURLBase, "http://localhost:8080")
}

func TestClarifyShortURLBaseEnableHttps(t *testing.T) {
	values := Config{}

	err := env.Parse(&values)
	require.NoError(t, err)

	values.EnableHTTPS = true

	err = values.clarifyShortURLBase()
	require.NoError(t, err)

	assert.Equal(t, values.ShortURLBase, "https://localhost")
}

func TestClarifyShortURLBaseEnableHttpsAltPort(t *testing.T) {
	values := Config{}

	err := env.Parse(&values)
	require.NoError(t, err)

	values.EnableHTTPS = true

	values.ShortURLBase = "http://localhost:447"

	err = values.clarifyShortURLBase()
	require.NoError(t, err)

	assert.Equal(t, values.ShortURLBase, "https://localhost:447")
}
