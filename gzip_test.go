package gziphandler

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	smallTestBody = "aaabbcaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbccc"
	testBody      = "aaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbcccaaabbbccc"
)

func TestGzipHandler(t *testing.T) {
	// This just exists to provide something for GzipHandler to wrap.
	handler := newTestHandler(testBody)

	// requests without accept-encoding are passed along as-is

	req1 := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	resp1 := httptest.NewRecorder()
	handler.ServeHTTP(resp1, req1)
	res1 := resp1.Result()

	assert.Equal(t, http.StatusOK, res1.StatusCode)
	assert.Equal(t, "", res1.Header.Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", res1.Header.Get("Vary"))
	assert.Equal(t, testBody, resp1.Body.String())

	// but requests with accept-encoding:gzip are compressed if possible

	req2 := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req2.Header.Set("Accept-Encoding", "gzip")
	resp2 := httptest.NewRecorder()
	handler.ServeHTTP(resp2, req2)
	res2 := resp2.Result()

	assert.Equal(t, http.StatusOK, res2.StatusCode)
	assert.Equal(t, "gzip", res2.Header.Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", res2.Header.Get("Vary"))
	assert.Equal(t, gzipStrLevel(testBody, DefaultCompression), resp2.Body.Bytes())
}

func TestGzipHandlerAcceptEncodingCaseInsensitive(t *testing.T) {
	// This just exists to provide something for GzipHandler to wrap.
	handler := newTestHandler(testBody)

	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req.Header.Set("Accept-Encoding", "GZIP")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	res := resp.Result()

	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "gzip", res.Header.Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", res.Header.Get("Vary"))
	assert.Equal(t, gzipStrLevel(testBody, DefaultCompression), resp.Body.Bytes())
}

func TestGzipLevelHandler(t *testing.T) {
	for lvl := BestSpeed; lvl <= BestCompression; lvl++ {
		req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp := httptest.NewRecorder()
		newTestHandler(testBody, CompressionLevel(lvl)).ServeHTTP(resp, req)
		res := resp.Result()

		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, "gzip", res.Header.Get("Content-Encoding"))
		assert.Equal(t, "Accept-Encoding", res.Header.Get("Vary"))
		assert.Equal(t, gzipStrLevel(testBody, lvl), resp.Body.Bytes())
	}
}

func TestCompressionLevelPanicsForInvalid(t *testing.T) {
	assert.PanicsWithValue(t, "gziphandler: invalid compression level requested", func() {
		CompressionLevel(-42)
	}, "CompressionLevel did not panic on invalid level")

	assert.PanicsWithValue(t, "gziphandler: invalid compression level requested", func() {
		CompressionLevel(42)
	}, "CompressionLevel did not panic on invalid level")
}

func TestGzipHandlerNoBody(t *testing.T) {
	tests := []struct {
		statusCode      int
		contentEncoding string
		bodyLen         int
	}{
		// Body must be empty.
		{http.StatusNoContent, "", 0},
		{http.StatusNotModified, "", 0},
		// Body is going to get gzip'd no matter what.
		{http.StatusOK, "", 0},
	}

	for num, test := range tests {
		handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(test.statusCode)
		}))

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		handler.ServeHTTP(rec, req)

		header := rec.Header()
		assert.Equal(t, test.contentEncoding, header.Get("Content-Encoding"), "for test iteration %d", num)
		assert.Equal(t, "Accept-Encoding", header.Get("Vary"), "for test iteration %d", num)
		assert.Equal(t, test.bodyLen, rec.Body.Len(), "for test iteration %d", num)
	}
}

func TestGzipHandlerContentLength(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test: no external network in -short mode")
	}

	b := []byte(testBody)
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Write(b)
	}), CompressionLevel(DefaultCompression))

	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	require.NoError(t, err, "Unexpected error making http request")
	req.Close = true
	req.Header.Set("Accept-Encoding", "gzip")

	// TODO: in go1.9 (*httptest.Server).Client was
	// introduced. This should be used once go1.8 is
	// no longer supported.
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "Unexpected error making http request")
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	require.NoError(t, err, "Unexpected error reading response body")

	l, err := strconv.Atoi(res.Header.Get("Content-Length"))
	require.NoError(t, err, "Unexpected error parsing Content-Length")
	assert.Len(t, body, l)
	assert.Equal(t, "gzip", res.Header.Get("Content-Encoding"))
	assert.Equal(t, gzipStrLevel(testBody, DefaultCompression), body)
}

func TestGzipHandlerMinSize(t *testing.T) {
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, _ := ioutil.ReadAll(r.Body)
		w.Write(resp)
		// Call write multiple times to pass through "chosenWriter"
		w.Write(resp)
		w.Write(resp)
	}), MinSize(13))

	// Run a test with size smaller than the limit
	b := bytes.NewBufferString("test")

	req1 := httptest.NewRequest(http.MethodGet, "/whatever", b)
	req1.Header.Add("Accept-Encoding", "gzip")
	resp1 := httptest.NewRecorder()
	handler.ServeHTTP(resp1, req1)
	res1 := resp1.Result()
	assert.Equal(t, "", res1.Header.Get("Content-Encoding"))

	// Run a test with size bigger than the limit
	b = bytes.NewBufferString(smallTestBody)

	req2 := httptest.NewRequest(http.MethodGet, "/whatever", b)
	req2.Header.Add("Accept-Encoding", "gzip")
	resp2 := httptest.NewRecorder()
	handler.ServeHTTP(resp2, req2)
	res2 := resp2.Result()
	assert.Equal(t, "gzip", res2.Header.Get("Content-Encoding"))
}

func TestMinSizePanicsForInvalid(t *testing.T) {
	assert.PanicsWithValue(t, "gziphandler: minimum size must not be negative", func() {
		MinSize(-10)
	}, "MinSize did not panic on negative size")
}

func TestGzipDoubleClose(t *testing.T) {
	h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// call close here and it'll get called again interally by
		// NewGzipLevelHandler's handler defer
		io.WriteString(w, "test")
		w.(io.Closer).Close()
	}), MinSize(0), CompressionLevel(DefaultCompression))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	// the second close shouldn't have added the same writer
	// so we pull out 2 writers from the pool and make sure they're different
	w1 := gzipWriterGet(nil, DefaultCompression)
	w2 := gzipWriterGet(nil, DefaultCompression)
	// assert.NotEqual looks at the value and not the address, so we use regular ==
	assert.False(t, w1 == w2)
}

func TestStatusCodes(t *testing.T) {
	handler := Gzip(http.NotFoundHandler())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	result := w.Result()
	assert.Equal(t, http.StatusNotFound, result.StatusCode)

	handler = Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	result = w.Result()
	assert.Equal(t, http.StatusNotFound, result.StatusCode)

	handler = Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	result = w.Result()
	assert.Equal(t, http.StatusOK, result.StatusCode)
}

type httpFlusherFunc func()

func (fn httpFlusherFunc) Flush() { fn() }

func TestFlush(t *testing.T) {
	b := []byte(testBody)
	handler := Gzip(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
		rw.Write(b)
		rw.(http.Flusher).Flush()
		rw.Write(b)
	}), MinSize(0), CompressionLevel(DefaultCompression))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()

	var flushed bool
	handler.ServeHTTP(struct {
		http.ResponseWriter
		http.Flusher
	}{
		w,
		httpFlusherFunc(func() { flushed = true }),
	}, r)

	assert.True(t, flushed, "Flush did not call underlying http.Flusher")

	res := w.Result()
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.Equal(t, "gzip", res.Header.Get("Content-Encoding"))

	var buf bytes.Buffer
	gw, _ := gzip.NewWriterLevel(&buf, DefaultCompression)

	gw.Write(b)
	gw.Flush() // Flush emits a symbol into the deflate output
	gw.Write(b)
	gw.Close()

	assert.Equal(t, buf.Bytes(), w.Body.Bytes())
}

func TestFlushBeforeWrite(t *testing.T) {
	b := []byte(testBody)
	handler := Gzip(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
		rw.(http.Flusher).Flush()
		rw.Write(b)
	}), CompressionLevel(DefaultCompression))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	res := w.Result()
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.Equal(t, "gzip", res.Header.Get("Content-Encoding"))
	assert.Equal(t, gzipStrLevel(testBody, DefaultCompression), w.Body.Bytes())
}

func TestInferContentType(t *testing.T) {
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<!doc")
		io.WriteString(w, "type html>")
	}), MinSize(len("<!doctype html")))

	req1 := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req1.Header.Add("Accept-Encoding", "gzip")
	resp1 := httptest.NewRecorder()
	handler.ServeHTTP(resp1, req1)

	res1 := resp1.Result()
	assert.Equal(t, "text/html; charset=utf-8", res1.Header.Get("Content-Type"))
}

func TestInferContentTypeUncompressed(t *testing.T) {
	handler := newTestHandler("<!doctype html>")

	req1 := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req1.Header.Add("Accept-Encoding", "gzip")
	resp1 := httptest.NewRecorder()
	handler.ServeHTTP(resp1, req1)

	res1 := resp1.Result()
	assert.Equal(t, "text/html; charset=utf-8", res1.Header.Get("Content-Type"))
}

func TestInferContentTypeBuffered(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("<!doctype html>")

	for i := len("<!doctype html>"); i < 512; i++ {
		buf.WriteByte('.')
	}

	require.Len(t, buf.Bytes(), 512, "invariant")

	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
		io.WriteString(w, "test")
	}), MinSize(513))

	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req.Header.Add("Accept-Encoding", "gzip")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	res := resp.Result()
	assert.Equal(t, "text/html; charset=utf-8", res.Header.Get("Content-Type"))

	buf.Truncate(511)

	handler = Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
		io.WriteString(w, "test")
	}), MinSize(512))

	req = httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req.Header.Add("Accept-Encoding", "gzip")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	res = resp.Result()
	assert.Equal(t, "text/html; charset=utf-8", res.Header.Get("Content-Type"))
}

type httpCloseNotifierFunc func() <-chan bool

func (fn httpCloseNotifierFunc) CloseNotify() <-chan bool { return fn() }

type httpHijackerFunc func() (net.Conn, *bufio.ReadWriter, error)

func (fn httpHijackerFunc) Hijack() (net.Conn, *bufio.ReadWriter, error) { return fn() }

type httpPusherFunc func(target string, opts *http.PushOptions) error

func (fn httpPusherFunc) Push(target string, opts *http.PushOptions) error { return fn(target, opts) }

func TestResponseWriterTypes(t *testing.T) {
	var closeNotified bool
	closeNotifier := func() http.CloseNotifier {
		closeNotified = false
		return httpCloseNotifierFunc(func() <-chan bool {
			closeNotified = true
			return nil
		})
	}

	var hijacked bool
	hijacker := func() http.Hijacker {
		hijacked = false
		return httpHijackerFunc(func() (net.Conn, *bufio.ReadWriter, error) {
			hijacked = true
			return nil, nil, nil
		})
	}

	var pushed bool
	pusher := func() http.Pusher {
		pushed = false
		return httpPusherFunc(func(string, *http.PushOptions) error {
			pushed = true
			return nil
		})
	}

	var cok, hok, pok bool
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c http.CloseNotifier
		if c, cok = w.(http.CloseNotifier); cok {
			c.CloseNotify()
		}

		var h http.Hijacker
		if h, hok = w.(http.Hijacker); hok {
			h.Hijack()
		}

		var p http.Pusher
		if p, pok = w.(http.Pusher); pok {
			p.Push("", nil)
		}
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req1.Header.Add("Accept-Encoding", "gzip")

	resp1 := httptest.NewRecorder()

	handler.ServeHTTP(resp1, req1)
	assert.True(t, !cok && !hok && !pok, "expected plain ResponseWriter")

	handler.ServeHTTP(struct {
		http.ResponseWriter
		http.CloseNotifier
	}{resp1, closeNotifier()}, req1)
	assert.True(t, cok && !hok && !pok, "expected CloseNotifier")
	assert.True(t, closeNotified, "CloseNotify did not call underlying http.CloseNotifier")

	handler.ServeHTTP(struct {
		http.ResponseWriter
		http.Hijacker
	}{resp1, hijacker()}, req1)
	assert.True(t, !cok && hok && !pok, "expected Hijacker")
	assert.True(t, hijacked, "Hijack did not call underlying http.Hijacker")

	handler.ServeHTTP(struct {
		http.ResponseWriter
		http.Pusher
	}{resp1, pusher()}, req1)
	assert.True(t, !cok && !hok && pok, "expected Pusher")
	assert.True(t, pushed, "Push did not call underlying http.Pusher")

	handler.ServeHTTP(struct {
		http.ResponseWriter
		http.CloseNotifier
		http.Hijacker
	}{resp1, closeNotifier(), hijacker()}, req1)
	assert.True(t, cok && hok && !pok, "expected CloseNotifier and Hijacker")
	assert.True(t, closeNotified, "CloseNotify did not call underlying http.CloseNotifier")
	assert.True(t, hijacked, "Hijack did not call underlying http.Hijacker")

	handler.ServeHTTP(struct {
		http.ResponseWriter
		http.CloseNotifier
		http.Pusher
	}{resp1, closeNotifier(), pusher()}, req1)
	assert.True(t, cok && !hok && pok, "expected CloseNotifier and Pusher")
	assert.True(t, closeNotified, "CloseNotify did not call underlying http.CloseNotifier")
	assert.True(t, pushed, "Push did not call underlying http.Pusher")
}

func TestContentTypes(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		contentType          string
		sniffContentType     bool
		acceptedContentTypes []string
		expectedGzip         bool
	}{
		{
			name:                 "Always gzip when content types are empty",
			contentType:          "",
			acceptedContentTypes: []string{},
			expectedGzip:         true,
		},
		{
			name:                 "Exact content-type match",
			contentType:          "application/json",
			acceptedContentTypes: []string{"application/json"},
			expectedGzip:         true,
		},
		{
			name:                 "Case-insensitive content-type matching",
			contentType:          "Application/Json",
			acceptedContentTypes: []string{"application/json"},
			expectedGzip:         true,
		},
		{
			name:                 "Non-matching content-type",
			contentType:          "text/xml",
			acceptedContentTypes: []string{"application/json"},
			expectedGzip:         false,
		},
		{
			name:                 "No-subtype content-type match",
			contentType:          "application/json",
			acceptedContentTypes: []string{"application/*"},
			expectedGzip:         true,
		},
		{
			name:                 "Case-insensitive no-subtype content-type match",
			contentType:          "Application/Json",
			acceptedContentTypes: []string{"application/*"},
			expectedGzip:         true,
		},
		{
			name:                 "content-type with directive match",
			contentType:          "application/json; charset=utf-8",
			acceptedContentTypes: []string{"application/json"},
			expectedGzip:         true,
		},

		{
			name:                 "Always gzip when content types are empty, sniffed",
			sniffContentType:     true,
			acceptedContentTypes: []string{},
			expectedGzip:         true,
		},
		{
			name:                 "Exact content-type match, sniffed",
			sniffContentType:     true,
			acceptedContentTypes: []string{"text/plain"},
			expectedGzip:         true,
		},
		{
			name:                 "Case-insensitive content-type matching",
			sniffContentType:     true,
			acceptedContentTypes: []string{"Text/Plain"},
			expectedGzip:         true,
		},
		{
			name:                 "Non-matching content-type, sniffed",
			sniffContentType:     true,
			acceptedContentTypes: []string{"application/json"},
			expectedGzip:         false,
		},
		{
			name:                 "No-subtype content-type match, sniffed",
			sniffContentType:     true,
			acceptedContentTypes: []string{"text/*"},
			expectedGzip:         true,
		},
		{
			name:                 "Case-insensitive no-subtype content-type match",
			sniffContentType:     true,
			acceptedContentTypes: []string{"Text/*"},
			expectedGzip:         true,
		},
	} {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !tt.sniffContentType {
				w.Header().Set("Content-Type", tt.contentType)
			}

			w.WriteHeader(http.StatusTeapot)
			io.WriteString(w, testBody)
		})

		req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp := httptest.NewRecorder()
		Gzip(handler, ContentTypes(tt.acceptedContentTypes)).ServeHTTP(resp, req)

		res := resp.Result()
		assert.Equal(t, http.StatusTeapot, res.StatusCode)

		if ce := res.Header.Get("Content-Encoding"); tt.expectedGzip {
			assert.Equal(t, "gzip", ce, tt.name)
		} else {
			assert.NotEqual(t, "gzip", ce, tt.name)
		}
	}
}

func TestContentTypesMultiWrite(t *testing.T) {
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "example/mismatch")
		io.WriteString(w, testBody)

		w.Header().Set("Content-Type", "example/match")
		io.WriteString(w, testBody)
	}), ContentTypes([]string{"example/match"}))

	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	res := resp.Result()
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.NotEqual(t, "gzip", res.Header.Get("Content-Encoding"))
	assert.Equal(t, testBody+testBody, resp.Body.String())
}

func TestContentTypesCopies(t *testing.T) {
	s := []string{"application/example"}

	var c config
	ContentTypes(s)(&c)

	require.NotEmpty(t, c.contentTypes)
	assert.False(t, &c.contentTypes[0] == &s[0], "ContentTypes returned same slice")
}

func TestGzipHandlerAlreadyCompressed(t *testing.T) {
	handler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "br")
		io.WriteString(w, testBody)
	}))

	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	assert.Equal(t, "br", res.Result().Header.Get("Content-Encoding"))
	assert.Equal(t, testBody, res.Body.String())
}

func TestReleaseBufferPanicsInvaraiant(t *testing.T) {
	assert.PanicsWithValue(t, "gziphandler: w.buf is nil in call to emptyBuffer", func() {
		new(responseWriter).releaseBuffer()
	}, "releaseBuffer did not panic with nil buf")
}

func TestWritePanicsInvariant(t *testing.T) {
	assert.PanicsWithValue(t, "gziphandler: both buf and gw are non nil in call to Write", func() {
		(&responseWriter{
			gw:  new(gzip.Writer),
			buf: new([]byte),
		}).Write(nil)
	}, "Write did not panic with both gw and buf non-nil")
}

func TestClosePanicsInvariant(t *testing.T) {
	assert.PanicsWithValue(t, "gziphandler: both buf and gw are non nil in call to Close", func() {
		(&responseWriter{
			gw:  new(gzip.Writer),
			buf: new([]byte),
		}).Close()
	}, "Close did not panic with both gw and buf non-nil")
}

// we use an int and not a struct{} as the latter is not
// guaranteed to have a unique address.
type dummyHTTPHandler int

func (dummyHTTPHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func TestWrapper(t *testing.T) {
	// we need a struct with a unique address for the
	// assert.Equal comparison.
	handler := new(dummyHTTPHandler)

	assert.Equal(t, Gzip(handler), Wrapper()(handler))
	assert.Equal(t, Gzip(handler, MinSize(42)), Wrapper(MinSize(42))(handler))
}

func TestShouldGzip(t *testing.T) {
	for _, tc := range []struct {
		shouldGzip ShouldGzipType
		advertise  bool
		expect     bool
	}{
		{NegotiateGzip, false, false},
		{NegotiateGzip, true, true},
		{SkipGzip, false, false},
		{SkipGzip, true, false},
		{ForceGzip, false, true},
		{ForceGzip, true, true},
	} {
		handler := newTestHandler(testBody, ShouldGzip(func(*http.Request) ShouldGzipType {
			return tc.shouldGzip
		}))

		req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
		if tc.advertise {
			req.Header.Set("Accept-Encoding", "gzip")
		}

		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)

		res := resp.Result()
		assert.Equal(t, http.StatusOK, res.StatusCode, "%+v", tc)
		assert.Equal(t, "Accept-Encoding", res.Header.Get("Vary"), "%+v", tc)

		if tc.expect {
			assert.Equal(t, "gzip", res.Header.Get("Content-Encoding"), "%+v", tc)
			assert.Equal(t, gzipStrLevel(testBody, DefaultCompression), resp.Body.Bytes(), "%+v", tc)
		} else {
			assert.Equal(t, "", res.Header.Get("Content-Encoding"), "%+v", tc)
			assert.Equal(t, testBody, resp.Body.String(), "%+v", tc)
		}
	}
}

// --------------------------------------------------------------------

func BenchmarkGzipHandler_S2k(b *testing.B)   { benchmark(b, false, 2048) }
func BenchmarkGzipHandler_S20k(b *testing.B)  { benchmark(b, false, 20480) }
func BenchmarkGzipHandler_S100k(b *testing.B) { benchmark(b, false, 102400) }
func BenchmarkGzipHandler_P2k(b *testing.B)   { benchmark(b, true, 2048) }
func BenchmarkGzipHandler_P20k(b *testing.B)  { benchmark(b, true, 20480) }
func BenchmarkGzipHandler_P100k(b *testing.B) { benchmark(b, true, 102400) }

// --------------------------------------------------------------------

func gzipStrLevel(s string, lvl int) []byte {
	var b bytes.Buffer
	w, _ := gzip.NewWriterLevel(&b, lvl)
	io.WriteString(w, s)
	w.Close()
	return b.Bytes()
}

func benchmark(b *testing.B, parallel bool, size int) {
	bin, err := ioutil.ReadFile("testdata/benchmark.json")
	require.NoError(b, err)

	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	handler := newTestHandler(string(bin[:size]))

	if parallel {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				runBenchmark(b, req, handler)
			}
		})
	} else {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			runBenchmark(b, req, handler)
		}
	}
}

func runBenchmark(b *testing.B, req *http.Request, handler http.Handler) {
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	require.Equal(b, http.StatusOK, res.Code)
	require.False(b, res.Body.Len() < 500, "Expected complete response body, but got %d bytes", res.Body.Len())
}

func newTestHandler(body string, opts ...Option) http.Handler {
	return Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}), opts...)
}
