// Package gzippedhttp provides utilities for handling gzip-compressed HTTP requests and responses.
// It includes wrappers for http.ResponseWriter and io.ReadCloser that transparently compress
// or decompress data using gzip format.
package gzippedhttp

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// CompressedReader wraps an io.ReadCloser and decompresses its input using gzip.
type CompressedReader struct {
	r  io.ReadCloser
	zr *gzip.Reader
}

// NewCompressedReader returns a new CompressedReader that reads gzip-compressed data
// from the provided io.ReadCloser.
func NewCompressedReader(requestBody io.ReadCloser) (*CompressedReader, error) {
	zippedRequestBody, err := gzip.NewReader(requestBody)
	if err != nil {
		return nil, err
	}

	return &CompressedReader{
		r:  requestBody,
		zr: zippedRequestBody,
	}, nil
}

// Read reads decompressed data from the underlying gzip stream.
func (c CompressedReader) Read(p []byte) (n int, err error) {
	return c.zr.Read(p)
}

// Close closes both the gzip reader and the underlying io.ReadCloser.
func (c *CompressedReader) Close() error {
	if err := c.r.Close(); err != nil {
		return err
	}
	return c.zr.Close()
}

// CompressedHTTPResponseWriter wraps http.ResponseWriter and compresses
// the response body using gzip.
type CompressedHTTPResponseWriter struct {
	w  http.ResponseWriter
	zw *gzip.Writer
}

// NewCompressedHTTPResponseWriter returns a new CompressedHTTPResponseWriter
// that writes gzip-compressed responses to the provided http.ResponseWriter.
func NewCompressedHTTPResponseWriter(w http.ResponseWriter) *CompressedHTTPResponseWriter {
	zw := gzipWriterPool.Get().(*gzip.Writer)
	zw.Reset(w)
	return &CompressedHTTPResponseWriter{
		w:  w,
		zw: zw,
	}
}

// Close closes both the gzip reader and the underlying io.ReadCloser.
func (c *CompressedHTTPResponseWriter) Close() error {
	err := c.zw.Close()
	if err != nil {
		return err
	}
	gzipWriterPool.Put(c.zw)
	return nil
}

// WriteHeader sets the HTTP status code for the response.
func (c *CompressedHTTPResponseWriter) WriteHeader(statusCode int) {
	if statusCode < 300 {
		c.w.Header().Set("Content-Encoding", "gzip")
	}
	c.w.WriteHeader(statusCode)
}

// Write writes gzip-compressed data to the response body.
func (c *CompressedHTTPResponseWriter) Write(p []byte) (int, error) {
	return c.zw.Write(p)
}

// Header returns the HTTP headers associated with the response.
func (c *CompressedHTTPResponseWriter) Header() http.Header {
	return c.w.Header()
}

var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
		return w
	},
}

// GzipResponse is the middleware that determines whether a response should be compressed based
// on the request's "Accept-Encoding" header.
func GzipResponse(h http.Handler) http.Handler {
	middleware := func(response http.ResponseWriter, request *http.Request) {
		finalResponse := response

		acceptEncoding := request.Header.Get("Accept-Encoding")
		clientAcceptsGzip := strings.Contains(acceptEncoding, "gzip")
		if clientAcceptsGzip {
			responseWithCompression := NewCompressedHTTPResponseWriter(response)
			finalResponse = responseWithCompression
			defer responseWithCompression.Close()
		}

		h.ServeHTTP(finalResponse, request)
	}

	return http.HandlerFunc(middleware)
}

// UngzipJSONAndTextHTMLRequest is a middleware function that decompresses
// gzip-encoded HTTP request bodies if the request's Content-Encoding is "gzip".
// It replaces the request body with a decompressed reader before passing the request
// to the next handler in the chain.
func UngzipJSONAndTextHTMLRequest(h http.Handler) http.Handler {
	middleware := func(response http.ResponseWriter, request *http.Request) {
		contentEncoding := request.Header.Get("Content-Encoding")
		clientSendsGzippedData := strings.Contains(contentEncoding, "gzip")
		if clientSendsGzippedData {
			requestBodyWithCompression, err := NewCompressedReader(request.Body)
			if err != nil {
				response.WriteHeader(http.StatusInternalServerError)
				return
			}
			request.Body = requestBodyWithCompression
			defer requestBodyWithCompression.Close()
		}

		h.ServeHTTP(response, request)
	}

	return http.HandlerFunc(middleware)
}
