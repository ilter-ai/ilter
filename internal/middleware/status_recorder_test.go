package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResponseRecorder(t *testing.T) {
	w := httptest.NewRecorder()
	rec := NewResponseRecorder(w)

	rec.WriteHeader(201)
	n, err := rec.Write([]byte("hello"))

	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, 201, rec.Status())
	assert.Equal(t, 5, rec.BytesWritten())
	assert.Equal(t, "hello", rec.BodyString())
	assert.Equal(t, 201, w.Code)
	assert.Equal(t, "hello", w.Body.String())
}
