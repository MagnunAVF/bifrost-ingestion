package embed_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/embed"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

func TestOllamaStatusError(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantBody string
	}{
		{name: "model not found", status: http.StatusNotFound, body: `{"error":"model \"x\" not found"}` + "\n", wantBody: `{"error":"model \"x\" not found"}`},
		{name: "server error", status: http.StatusInternalServerError, body: "boom", wantBody: "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			_, err := newOllama(t, srv, embed.OllamaConfig{}).Embed(t.Context(), []string{"1"})

			require.ErrorIs(t, err, errs.ErrUpstream)
			var se *embed.StatusError
			require.True(t, errors.As(err, &se), "got %v", err)
			assert.Equal(t, tt.status, se.Code)
			assert.Equal(t, tt.wantBody, se.Body)
		})
	}
}
