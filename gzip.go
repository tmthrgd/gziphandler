package gziphandler

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const (
	vary            = "Vary"
	acceptEncoding  = "Accept-Encoding"
	contentEncoding = "Content-Encoding"
	contentType     = "Content-Type"
	contentLength   = "Content-Length"
)

type codings map[string]float64

const (
	// defaultQValue is the default qvalue to assign to an encoding if no explicit qvalue is set.
	// This is actually kind of ambiguous in RFC 2616, so hopefully it's correct.
	// The examples seem to indicate that it is.
	defaultQValue = 1.0

	// defaultMinSize defines the minimum size to reach to enable compression.
	// It's 512 bytes.
	defaultMinSize = 512
)

// gzipWriterPools stores a sync.Pool for each compression level for reuse of
// gzip.Writers. Use poolIndex to covert a compression level to an index into
// gzipWriterPools.
var gzipWriterPools [gzip.BestCompression - gzip.BestSpeed + 2]*sync.Pool

func init() {
	for i := gzip.BestSpeed; i <= gzip.BestCompression; i++ {
		addLevelPool(i)
	}
	addLevelPool(gzip.DefaultCompression)
}

// poolIndex maps a compression level to its index into gzipWriterPools. It
// assumes that level is a valid gzip compression level.
func poolIndex(level int) int {
	// gzip.DefaultCompression == -1, so we need to treat it special.
	if level == gzip.DefaultCompression {
		return gzip.BestCompression - gzip.BestSpeed + 1
	}
	return level - gzip.BestSpeed
}

func addLevelPool(level int) {
	gzipWriterPools[poolIndex(level)] = &sync.Pool{
		New: func() interface{} {
			// NewWriterLevel only returns error on a bad level, we are guaranteeing
			// that this will be a valid level so it is okay to ignore the returned
			// error.
			w, _ := gzip.NewWriterLevel(nil, level)
			return w
		},
	}
}

// GzipResponseWriter provides an http.ResponseWriter interface, which gzips
// bytes before writing them to the underlying response. This doesn't close the
// writers, so don't forget to do that.
// It can be configured to skip response smaller than minSize.
type GzipResponseWriter struct {
	http.ResponseWriter
	index int // Index for gzipWriterPools.
	gw    *gzip.Writer

	code int // Saves the WriteHeader value.

	minSize      int    // Specifed the minimum response size to gzip. If the response length is bigger than this value, it is compressed.
	buf          []byte // Holds the first part of the write before reaching the minSize or the end of the write.
	bytesWritten int    // Keep trace of the numbers of bytes written.
}

// Write appends data to the gzip writer.
func (w *GzipResponseWriter) Write(b []byte) (int, error) {
	// If content type is not set.
	if _, ok := w.Header()[contentType]; !ok {
		// It infer it from the uncompressed body.
		w.Header().Set(contentType, http.DetectContentType(b))
	}

	// GZIP responseWriter is initialized. Use the GZIP responseWriter.
	if w.gw != nil {
		n, err := w.gw.Write(b)

		// Update the numbers of bytes written.
		w.bytesWritten += n

		return n, err
	}

	// Save the write into a buffer for later use in GZIP responseWriter (if content is long enough) or at close with regular responseWriter.
	w.buf = append(w.buf, b...)

	// If the global writes are bigger than the minSize, compression is enable.
	if w.bytesWritten+len(b) > w.minSize {
		return w.startGzip()
	}

	return len(b), nil
}

// startGzip initialize any GZIP specific informations.
func (w *GzipResponseWriter) startGzip() (int, error) {
	// Set the GZIP header.
	w.Header().Set(contentEncoding, "gzip")

	// if the Content-Length is already set, then calls to Write on gzip
	// will fail to set the Content-Length header since its already set
	// See: https://github.com/golang/go/issues/14975.
	w.Header().Del(contentLength)

	// Write the header to gzip response.
	w.writeHeader()

	// Initialize the GZIP response.
	w.init()

	// Flush the buffer into the gzip reponse.
	n, err := w.gw.Write(w.buf)
	// Empty the buffer.
	w.buf = nil

	// Return the numbers of bytes writen and the error if any.
	return n, err
}

// WriteHeader just saves the response code until close or GZIP effective writes.
func (w *GzipResponseWriter) WriteHeader(code int) {
	w.code = code
}

// writeHeader uses the saved code to send it to the ResponseWriter.
func (w *GzipResponseWriter) writeHeader() {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(w.code)
}

// init graps a new gzip writer from the gzipWriterPool and writes the correct
// content encoding header.
func (w *GzipResponseWriter) init() {
	// Bytes written during ServeHTTP are redirected to this gzip writer
	// before being written to the underlying response.
	gzw := gzipWriterPools[w.index].Get().(*gzip.Writer)
	gzw.Reset(w.ResponseWriter)
	w.gw = gzw
}

// Close will close the gzip.Writer and will put it back in the gzipWriterPool.
func (w *GzipResponseWriter) Close() error {
	// Buffer not nil means the regular response must be returned.
	if w.buf != nil {
		w.writeHeader()
		// Make the write into the regular response.
		_, writeErr := w.ResponseWriter.Write(w.buf)
		// Returns the error if any at write.
		if writeErr != nil {
			return fmt.Errorf("gziphandler: write to regular responseWriter at close gets error: %q", writeErr.Error())
		}
	}

	// If the GZIP responseWriter is not set no needs to close it.
	if w.gw == nil {
		return nil
	}

	err := w.gw.Close()
	gzipWriterPools[w.index].Put(w.gw)
	w.gw = nil
	return err
}

// Flush flushes the underlying *gzip.Writer and then the underlying
// http.ResponseWriter if it is an http.Flusher. This makes GzipResponseWriter
// an http.Flusher.
func (w *GzipResponseWriter) Flush() {
	if w.gw != nil {
		w.gw.Flush()
	}

	if fw, ok := w.ResponseWriter.(http.Flusher); ok {
		fw.Flush()
	}
}

// Hijack implements http.Hijacker. If the underlying ResponseWriter is a
// Hijacker, its Hijack method is returned. Otherwise an error is returned.
func (w *GzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("http.Hijacker interface is not supported")
}

// verify Hijacker interface implementation
var _ http.Hijacker = &GzipResponseWriter{}

// Push initiates an HTTP/2 server push.
// Push returns ErrNotSupported if the client has disabled push or if push
// is not supported on the underlying connection.
func (w *GzipResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if ok && pusher != nil {
		return pusher.Push(target, setAcceptEncodingForPushOptions(opts))
	}
	return http.ErrNotSupported
}

func newGzipLevelAndMinSize(level, minSize int) (func(http.Handler) http.Handler, error) {
	if level != gzip.DefaultCompression && (level < gzip.BestSpeed || level > gzip.BestCompression) {
		return nil, fmt.Errorf("invalid compression level requested: %d", level)
	}
	if minSize < 0 {
		return nil, fmt.Errorf("minimum size must be more than zero")
	}
	return func(h http.Handler) http.Handler {
		index := poolIndex(level)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add(vary, acceptEncoding)

			if acceptsGzip(r) {
				gw := &GzipResponseWriter{
					ResponseWriter: w,
					index:          index,
					minSize:        minSize,

					buf: []byte{},
				}
				defer gw.Close()

				h.ServeHTTP(gw, r)
			} else {
				h.ServeHTTP(w, r)
			}
		})
	}, nil
}

// GzipHandler wraps an HTTP handler, to transparently gzip the response body if
// the client supports it (via the Accept-Encoding header). This will compress at
// the default compression level.
func GzipHandler(h http.Handler) http.Handler {
	h, err := GzipHandlerWithLevel(h, gzip.DefaultCompression)
	if err != nil {
		panic(err)
	}

	return h
}

// GzipHandlerWithLevel wraps an HTTP handler, to transparently gzip the response
// body if the client supports it (via the Accept-Encoding header). This will compress
// at the given gzip compression level.
func GzipHandlerWithLevel(h http.Handler, level int) (http.Handler, error) {
	return GzipHandlerWithLevelAndMinSize(h, level, defaultMinSize)
}

// GzipHandlerWithLevelAndMinSize wraps an HTTP handler, to transparently gzip the
// response body if the client supports it (via the Accept-Encoding header). This will
// compress at the given gzip compression level. The resource will not be compressed
// unless it is larger than minSize.
func GzipHandlerWithLevelAndMinSize(h http.Handler, level, minSize int) (http.Handler, error) {
	wrapper, err := newGzipLevelAndMinSize(level, minSize)
	if err != nil {
		return nil, err
	}

	return wrapper(h), nil
}

// acceptsGzip returns true if the given HTTP request indicates that it will
// accept a gzipped response.
func acceptsGzip(r *http.Request) bool {
	acceptedEncodings, _ := parseEncodings(r.Header.Get(acceptEncoding))
	return acceptedEncodings["gzip"] > 0.0
}

// parseEncodings attempts to parse a list of codings, per RFC 2616, as might
// appear in an Accept-Encoding header. It returns a map of content-codings to
// quality values, and an error containing the errors encountered. It's probably
// safe to ignore those, because silently ignoring errors is how the internet
// works.
//
// See: http://tools.ietf.org/html/rfc2616#section-14.3.
func parseEncodings(s string) (codings, error) {
	c := make(codings)
	var e []string

	for _, ss := range strings.Split(s, ",") {
		coding, qvalue, err := parseCoding(ss)

		if err != nil {
			e = append(e, err.Error())
		} else {
			c[coding] = qvalue
		}
	}

	// TODO (adammck): Use a proper multi-error struct, so the individual errors
	//                 can be extracted if anyone cares.
	if len(e) > 0 {
		return c, fmt.Errorf("errors while parsing encodings: %s", strings.Join(e, ", "))
	}

	return c, nil
}

// parseCoding parses a single conding (content-coding with an optional qvalue),
// as might appear in an Accept-Encoding header. It attempts to forgive minor
// formatting errors.
func parseCoding(s string) (coding string, qvalue float64, err error) {
	for n, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		qvalue = defaultQValue

		if n == 0 {
			coding = strings.ToLower(part)
		} else if strings.HasPrefix(part, "q=") {
			qvalue, err = strconv.ParseFloat(strings.TrimPrefix(part, "q="), 64)

			if qvalue < 0.0 {
				qvalue = 0.0
			} else if qvalue > 1.0 {
				qvalue = 1.0
			}
		}
	}

	if coding == "" {
		err = fmt.Errorf("empty content-coding")
	}

	return
}

// setAcceptEncodingForPushOptions sets "Accept-Encoding" : "gzip" for PushOptions without overriding existing headers.
func setAcceptEncodingForPushOptions(opts *http.PushOptions) *http.PushOptions {

	if opts == nil {
		opts = &http.PushOptions{
			Header: http.Header{
				acceptEncoding: []string{"gzip"},
			},
		}
		return opts
	}

	if opts.Header == nil {
		opts.Header = http.Header{
			acceptEncoding: []string{"gzip"},
		}
		return opts
	}

	if encoding := opts.Header.Get(acceptEncoding); encoding == "" {
		opts.Header.Add(acceptEncoding, "gzip")
		return opts
	}

	return opts
}
