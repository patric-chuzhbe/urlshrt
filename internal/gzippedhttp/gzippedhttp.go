package gzippedhttp

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
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
	return c.zw.Close()
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

func NewCompressedHTTPResponseWriter(w http.ResponseWriter) *CompressedHTTPResponseWriter {
	return &CompressedHTTPResponseWriter{
		w:  w,
		zw: gzip.NewWriter(w),
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

func checkIfRequestContentTypeIsJSONOrTextHTML(request *http.Request) bool {
	contentType := request.Header.Get("Content-Type")
	return strings.Contains(contentType, "application/json") || strings.Contains(contentType, "text/html")
}

func UngzipJSONAndTextHTMLRequest(h http.Handler) http.Handler {
	middleware := func(response http.ResponseWriter, request *http.Request) {
		contentEncoding := request.Header.Get("Content-Encoding")
		clientSendsGzippedData := strings.Contains(contentEncoding, "gzip")
		// requestContentTypeIsJSONOrTextHTML := checkIfRequestContentTypeIsJSONOrTextHTML(request)
		if clientSendsGzippedData /*&& requestContentTypeIsJSONOrTextHTML*/ {
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
