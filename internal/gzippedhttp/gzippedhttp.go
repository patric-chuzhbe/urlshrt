package gzippedhttp

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

type CompressedReader struct {
	r  io.ReadCloser
	zr *gzip.Reader
}

func (c CompressedReader) Read(p []byte) (n int, err error) {
	return c.zr.Read(p)
}

func (c *CompressedReader) Close() error {
	if err := c.r.Close(); err != nil {
		return err
	}
	return c.zr.Close()
}

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

type CompressedHTTPResponseWriter struct {
	w  http.ResponseWriter
	zw *gzip.Writer
}

func (c *CompressedHTTPResponseWriter) Close() error {
	err := c.zw.Close()
	if err != nil {
		return err
	}
	gzipWriterPool.Put(c.zw)
	return nil
}

func (c *CompressedHTTPResponseWriter) WriteHeader(statusCode int) {
	if statusCode < 300 {
		c.w.Header().Set("Content-Encoding", "gzip")
	}
	c.w.WriteHeader(statusCode)
}

func (c *CompressedHTTPResponseWriter) Write(p []byte) (int, error) {
	return c.zw.Write(p)
}

func (c *CompressedHTTPResponseWriter) Header() http.Header {
	return c.w.Header()
}

var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
		return w
	},
}

func NewCompressedHTTPResponseWriter(w http.ResponseWriter) *CompressedHTTPResponseWriter {
	zw := gzipWriterPool.Get().(*gzip.Writer)
	zw.Reset(w)
	return &CompressedHTTPResponseWriter{
		w:  w,
		zw: zw,
	}
}

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
