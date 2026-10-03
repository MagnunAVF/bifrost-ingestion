package embed_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/embed"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

var _ embed.Embedder = (*embed.Ollama)(nil)

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// recorder is an Ollama stand-in. Each input text must be an integer n; its vector is [n, 1].
type recorder struct {
	mu       sync.Mutex
	requests []embedRequest
}

func (rec *recorder) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		rec.mu.Lock()
		rec.requests = append(rec.requests, req)
		rec.mu.Unlock()

		vecs := make([][]float32, len(req.Input))
		for i, in := range req.Input {
			n, err := strconv.Atoi(in)
			require.NoError(t, err)
			vecs[i] = []float32{float32(n), 1}
		}
		writeJSON(w, map[string]any{"model": req.Model, "embeddings": vecs})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func numbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strconv.Itoa(i)
	}
	return out
}

func newOllama(t *testing.T, srv *httptest.Server, cfg embed.OllamaConfig) *embed.Ollama {
	t.Helper()
	cfg.BaseURL = srv.URL
	if cfg.Model == "" {
		cfg.Model = "test-model"
	}
	o, err := embed.NewOllama(srv.Client(), cfg)
	require.NoError(t, err)
	return o
}

func TestOllamaEmbedHappyPath(t *testing.T) {
	var gotMethod, gotPath, gotType string
	rec := &recorder{}
	h := rec.handler(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		h(w, r)
	}))
	t.Cleanup(srv.Close)

	got, err := newOllama(t, srv, embed.OllamaConfig{Model: "nomic-embed-text"}).
		Embed(t.Context(), []string{"7", "3", "5"})
	require.NoError(t, err)

	assert.Equal(t, [][]float32{{7, 1}, {3, 1}, {5, 1}}, got)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/api/embed", gotPath)
	assert.Equal(t, "application/json", gotType)
	require.Len(t, rec.requests, 1)
	assert.Equal(t, embedRequest{Model: "nomic-embed-text", Input: []string{"7", "3", "5"}}, rec.requests[0])
}

func TestOllamaEmbedBatches(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler(t))
	t.Cleanup(srv.Close)

	texts := numbered(130)
	got, err := newOllama(t, srv, embed.OllamaConfig{BatchSize: 64}).Embed(t.Context(), texts)
	require.NoError(t, err)

	require.Len(t, got, 130)
	for i, v := range got {
		assert.Equal(t, []float32{float32(i), 1}, v, "vector %d out of order", i)
	}
	require.Len(t, rec.requests, 3)
	assert.Len(t, rec.requests[0].Input, 64)
	assert.Len(t, rec.requests[1].Input, 64)
	assert.Equal(t, []string{"128", "129"}, rec.requests[2].Input)
}

func TestOllamaEmbedSendsNothing(t *testing.T) {
	tests := []struct {
		name    string
		ctx     func(context.Context) context.Context
		texts   []string
		wantErr error
	}{
		{name: "nil input", texts: nil},
		{name: "empty input", texts: []string{}},
		{name: "empty text", texts: []string{"1", ""}, wantErr: errs.ErrInvalidInput},
		{name: "blank text", texts: []string{" \t ", "1"}, wantErr: errs.ErrInvalidInput},
		{
			name: "context already cancelled",
			ctx: func(ctx context.Context) context.Context {
				ctx, cancel := context.WithCancel(ctx)
				cancel()
				return ctx
			},
			texts:   []string{"1"},
			wantErr: context.Canceled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				calls.Add(1)
			}))
			t.Cleanup(srv.Close)

			ctx := t.Context()
			if tt.ctx != nil {
				ctx = tt.ctx(ctx)
			}
			got, err := newOllama(t, srv, embed.OllamaConfig{}).Embed(ctx, tt.texts)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Empty(t, got)
			assert.Zero(t, calls.Load(), "no request expected")
		})
	}
}

func TestOllamaEmbedUpstreamErrors(t *testing.T) {
	tests := []struct {
		name     string
		cfg      embed.OllamaConfig
		texts    []string
		status   int
		body     string
		wantText []string
	}{
		{
			name:     "server error",
			status:   http.StatusInternalServerError,
			body:     `{"error":"llama runner process has terminated"}`,
			wantText: []string{"500", "llama runner process has terminated"},
		},
		{
			name:     "model not found",
			status:   http.StatusNotFound,
			body:     `{"error":"model \"nope\" not found, try pulling it first"}`,
			wantText: []string{"404", "not found, try pulling it first"},
		},
		{
			name:     "long error body is cut",
			status:   http.StatusBadGateway,
			body:     strings.Repeat("x", 10_000),
			wantText: []string{"502"},
		},
		{name: "malformed json", body: `{"embeddings": [[1,2],`, wantText: []string{"decod"}},
		{name: "not json", body: `<html>proxy</html>`, wantText: []string{"decod"}},
		{
			name:     "fewer embeddings than inputs",
			texts:    []string{"1", "2"},
			body:     `{"embeddings":[[1,2]]}`,
			wantText: []string{"count mismatch"},
		},
		{
			name:     "more embeddings than inputs",
			body:     `{"embeddings":[[1,2],[3,4]]}`,
			wantText: []string{"count mismatch"},
		},
		{name: "missing embeddings", body: `{"model":"m"}`, wantText: []string{"count mismatch"}},
		{
			name:     "mixed dimensions",
			texts:    []string{"1", "2"},
			body:     `{"embeddings":[[1,2],[3,4,5]]}`,
			wantText: []string{"dimension"},
		},
		{name: "empty vector", body: `{"embeddings":[[]]}`, wantText: []string{"empty"}},
		{
			name:     "body over the limit",
			cfg:      embed.OllamaConfig{MaxResponseBytes: 64},
			body:     `{"embeddings":[[` + strings.Repeat("0.1234567,", 20) + `0]]}`,
			wantText: []string{"too large"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			texts := tt.texts
			if texts == nil {
				texts = []string{"1"}
			}
			got, err := newOllama(t, srv, tt.cfg).Embed(t.Context(), texts)
			require.ErrorIs(t, err, errs.ErrUpstream)
			assert.Nil(t, got)
			for _, want := range tt.wantText {
				assert.Contains(t, err.Error(), want)
			}
			assert.Less(t, len(err.Error()), 1024, "error message must stay short")
		})
	}
}

func TestOllamaEmbedDimensionChangeAcrossCalls(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			writeJSON(w, map[string]any{"embeddings": [][]float32{{1, 2, 3}}})
			return
		}
		writeJSON(w, map[string]any{"embeddings": [][]float32{{1, 2}}})
	}))
	t.Cleanup(srv.Close)

	o := newOllama(t, srv, embed.OllamaConfig{})
	_, err := o.Embed(t.Context(), []string{"a"})
	require.NoError(t, err)

	got, err := o.Embed(t.Context(), []string{"b"})
	require.ErrorIs(t, err, errs.ErrUpstream)
	assert.Contains(t, err.Error(), "dimension")
	assert.Nil(t, got)
}

func TestOllamaEmbedLaterBatchFails(t *testing.T) {
	var calls atomic.Int32
	rec := &recorder{}
	h := rec.handler(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 2 {
			http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)

	got, err := newOllama(t, srv, embed.OllamaConfig{BatchSize: 2}).Embed(t.Context(), numbered(5))
	require.ErrorIs(t, err, errs.ErrUpstream)
	assert.Contains(t, err.Error(), "batch 2/3")
	assert.Nil(t, got, "no partial result")
	assert.EqualValues(t, 2, calls.Load(), "stops at the failing batch")
}

// blockingServer holds every request until the client goes away and reports when one arrives.
func blockingServer(t *testing.T) (*httptest.Server, <-chan struct{}) {
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// The server only notices a closed connection once the body has been consumed.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	return srv, started
}

func TestOllamaEmbedTimeout(t *testing.T) {
	srv, _ := blockingServer(t)
	o := newOllama(t, srv, embed.OllamaConfig{Timeout: 50 * time.Millisecond})

	start := time.Now()
	got, err := o.Embed(t.Context(), []string{"1"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, got)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestOllamaEmbedCancelledInFlight(t *testing.T) {
	srv, started := blockingServer(t)
	o := newOllama(t, srv, embed.OllamaConfig{})

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-started
		cancel()
	}()

	start := time.Now()
	got, err := o.Embed(ctx, []string{"1"})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestOllamaEmbedConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	o := newOllama(t, srv, embed.OllamaConfig{})
	srv.Close()

	_, err := o.Embed(t.Context(), []string{"1"})
	require.ErrorIs(t, err, errs.ErrUpstream)
}

func TestNewOllama(t *testing.T) {
	tests := []struct {
		name    string
		cfg     embed.OllamaConfig
		wantErr string
	}{
		{name: "defaults", cfg: embed.OllamaConfig{Model: "nomic-embed-text"}},
		{name: "explicit https", cfg: embed.OllamaConfig{Model: "m", BaseURL: "https://ollama.internal:443/prefix"}},
		{name: "missing model", cfg: embed.OllamaConfig{}, wantErr: "model"},
		{name: "blank model", cfg: embed.OllamaConfig{Model: "  "}, wantErr: "model"},
		{name: "relative base url", cfg: embed.OllamaConfig{Model: "m", BaseURL: "localhost:11434"}, wantErr: "base url"},
		{name: "non-http base url", cfg: embed.OllamaConfig{Model: "m", BaseURL: "ftp://host"}, wantErr: "base url"},
		{name: "unparsable base url", cfg: embed.OllamaConfig{Model: "m", BaseURL: "http://[::1"}, wantErr: "base url"},
		{name: "negative timeout", cfg: embed.OllamaConfig{Model: "m", Timeout: -time.Second}, wantErr: "timeout"},
		{name: "negative batch size", cfg: embed.OllamaConfig{Model: "m", BatchSize: -1}, wantErr: "batch size"},
		{name: "negative body limit", cfg: embed.OllamaConfig{Model: "m", MaxResponseBytes: -1}, wantErr: "max response bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := embed.NewOllama(nil, tt.cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				require.NotNil(t, o)
				return
			}
			require.ErrorIs(t, err, errs.ErrInvalidInput)
			assert.Contains(t, strings.ToLower(err.Error()), tt.wantErr)
			assert.Nil(t, o)
		})
	}
}

func TestNewOllamaDefaults(t *testing.T) {
	o, err := embed.NewOllama(nil, embed.OllamaConfig{Model: "nomic-embed-text"})
	require.NoError(t, err)
	assert.Equal(t, embed.OllamaConfig{
		BaseURL:          "http://localhost:11434",
		Model:            "nomic-embed-text",
		Timeout:          2 * time.Minute,
		BatchSize:        64,
		MaxResponseBytes: 32 << 20,
	}, o.Config())
}

func TestOllamaEmbedBaseURLPrefix(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeJSON(w, map[string]any{"embeddings": [][]float32{{1}}})
	}))
	t.Cleanup(srv.Close)

	o, err := embed.NewOllama(srv.Client(), embed.OllamaConfig{Model: "m", BaseURL: srv.URL + "/ollama/"})
	require.NoError(t, err)
	_, err = o.Embed(t.Context(), []string{"x"})
	require.NoError(t, err)
	assert.Equal(t, "/ollama/api/embed", path, fmt.Sprintf("base %q", srv.URL))
}
