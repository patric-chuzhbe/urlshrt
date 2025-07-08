package config

import (
	"os"
	"testing"

	"github.com/caarlos0/env/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClarifyShortURLBaseDisableHttps(t *testing.T) {
	values := Config{}

	applyDefaults(&values, defaultConfig)

	err := values.clarifyShortURLBase()
	require.NoError(t, err)

	assert.Equal(t, "http://localhost:8080", values.ShortURLBase)
}

func TestClarifyShortURLBaseEnableHttps(t *testing.T) {
	values := Config{}

	applyDefaults(&values, defaultConfig)

	values.EnableHTTPS = true

	err := values.clarifyShortURLBase()
	require.NoError(t, err)

	assert.Equal(t, "https://localhost", values.ShortURLBase)
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

const testJSON = `{
	"server_address": ":3000",
	"base_url": "http://json-config.com",
	"file_storage_path": "json_storage.json",
	"database_dsn": "json-dsn",
	"enable_https": true
}`

func writeTempJSON(t *testing.T, content string) string {
	t.Helper()
	file, err := os.CreateTemp("", "config*.json")
	require.NoError(t, err)
	_, err = file.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	t.Cleanup(func() {
		err := os.Remove(file.Name())
		require.NoError(t, err)
	})
	return file.Name()
}

func TestConfigPriorityJSONOnly(t *testing.T) {
	jsonPath := writeTempJSON(t, testJSON)
	t.Setenv("CONFIG", jsonPath)

	cfg, err := New(WithDisableFlagsParsing(true))
	require.NoError(t, err)

	assert.Equal(t, ":3000", cfg.RunAddr)
	assert.Equal(t, "https://json-config.com", cfg.ShortURLBase)
	assert.Equal(t, "json_storage.json", cfg.DBFileName)
	assert.Equal(t, "json-dsn", cfg.DatabaseDSN)
	assert.True(t, cfg.EnableHTTPS)
}

func TestConfigPriorityJSONPlusEnv(t *testing.T) {
	jsonPath := writeTempJSON(t, testJSON)
	t.Setenv("CONFIG", jsonPath)
	t.Setenv("SERVER_ADDRESS", ":4000")
	t.Setenv("BASE_URL", "http://env.com")

	cfg, err := New(WithDisableFlagsParsing(true))
	require.NoError(t, err)

	assert.Equal(t, ":4000", cfg.RunAddr) // env overrides json
	assert.Equal(t, "https://env.com", cfg.ShortURLBase)
	assert.Equal(t, "json-dsn", cfg.DatabaseDSN) // from JSON
}

func TestConfigPriorityAllSources(t *testing.T) {
	jsonPath := writeTempJSON(t, testJSON)
	t.Setenv("CONFIG", jsonPath)
	t.Setenv("SERVER_ADDRESS", ":4000")
	t.Setenv("BASE_URL", "http://env.com")

	os.Args = []string{
		"testbin",
		"-a", ":6000",
		"-b", "http://cli.com",
	}

	cfg, err := New()
	require.NoError(t, err)

	assert.Equal(t, ":6000", cfg.RunAddr) // CLI > ENV > JSON
	assert.Equal(t, "https://cli.com", cfg.ShortURLBase)
	assert.Equal(t, "json-dsn", cfg.DatabaseDSN) // from JSON
}

func TestConfigEnvOnly(t *testing.T) {
	t.Setenv("SERVER_ADDRESS", ":7000")
	t.Setenv("BASE_URL", "http://envonly.com")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := New(WithDisableFlagsParsing(true))
	require.NoError(t, err)

	assert.Equal(t, ":7000", cfg.RunAddr)
	assert.Equal(t, "http://envonly.com", cfg.ShortURLBase)
	assert.Equal(t, "debug", cfg.LogLevel)
}
