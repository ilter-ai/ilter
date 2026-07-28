package middleware

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
)

// ResponseRecorder wraps http.ResponseWriter to capture status, body, and bytes written.
// Implements http.Flusher and http.Hijacker when the underlying writer does.
type ResponseRecorder struct {
	http.ResponseWriter
	body         *bytes.Buffer
	statusCode   int
	bytesWritten int
}

func NewResponseRecorder(w http.ResponseWriter) *ResponseRecorder {
	return &ResponseRecorder{
		ResponseWriter: w,
		body:           &bytes.Buffer{},
		statusCode:     http.StatusOK,
	}
}

func (r *ResponseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *ResponseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	n, err := r.ResponseWriter.Write(b)
	r.bytesWritten += n
	return n, err
}

func (r *ResponseRecorder) Status() int {
	return r.statusCode
}

func (r *ResponseRecorder) BytesWritten() int {
	return r.bytesWritten
}

func (r *ResponseRecorder) Body() []byte {
	return r.body.Bytes()
}

func (r *ResponseRecorder) BodyString() string {
	return r.body.String()
}

func (r *ResponseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *ResponseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("underlying ResponseWriter does not implement http.Hijacker")
}
