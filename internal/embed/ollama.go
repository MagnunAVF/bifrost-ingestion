package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

// Defaults applied by NewOllama to zero-valued OllamaConfig fields.
const (
	DefaultBaseURL          = "http://localhost:11434"
	DefaultTimeout          = 2 * time.Minute // the first request loads the model and is slow
	DefaultBatchSize        = 64
	DefaultMaxResponseBytes = 32 << 20
)

// errorSnippetBytes is how much of a non-200 body goes into the error message.
const errorSnippetBytes = 512

// OllamaConfig configures the Ollama client. Zero values take the Default* constants, except
// Model, which is required.
type OllamaConfig struct {
	BaseURL          string        // e.g. "http://localhost:11434"; may carry a path prefix
	Model            string        // e.g. "nomic-embed-text"
	Timeout          time.Duration // per HTTP request, including reading the body
	BatchSize        int           // texts per POST /api/embed
	MaxResponseBytes int64         // a larger response body is an error
}

// Ollama is an Embedder backed by a local Ollama server's POST /api/embed. It is safe for
// concurrent use.
type Ollama struct {
	client   *http.Client
	cfg      OllamaConfig
	endpoint string

	mu  sync.Mutex
	dim int // dimension of the first vector seen; 0 until then
}

var _ Embedder = (*Ollama)(nil)

// NewOllama validates cfg and fills in its defaults. A nil client means a fresh http.Client
// without a client-level timeout: Timeout is applied per request through the context, so a
// timeout satisfies errors.Is(err, context.DeadlineExceeded).
func NewOllama(client *http.Client, cfg OllamaConfig) (*Ollama, error) {
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("ollama config: model is required: %w", errs.ErrInvalidInput)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("ollama config: base URL %q must be an absolute http(s) URL: %w",
			cfg.BaseURL, errs.ErrInvalidInput)
	}
	switch {
	case cfg.Timeout < 0:
		return nil, fmt.Errorf("ollama config: timeout %v is negative: %w", cfg.Timeout, errs.ErrInvalidInput)
	case cfg.BatchSize < 0:
		return nil, fmt.Errorf("ollama config: batch size %d is negative: %w", cfg.BatchSize, errs.ErrInvalidInput)
	case cfg.MaxResponseBytes < 0:
		return nil, fmt.Errorf("ollama config: max response bytes %d is negative: %w",
			cfg.MaxResponseBytes, errs.ErrInvalidInput)
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if client == nil {
		client = &http.Client{}
	}
	return &Ollama{client: client, cfg: cfg, endpoint: u.JoinPath("api", "embed").String()}, nil
}

// Config returns the effective configuration, with defaults filled in.
func (o *Ollama) Config() OllamaConfig { return o.cfg }

// Embed sends texts in batches of BatchSize and returns their vectors in input order. Blank
// texts are rejected before any request. Any failing batch fails the whole call: no partial
// result is returned.
func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	for i, t := range texts {
		if strings.TrimSpace(t) == "" {
			return nil, fmt.Errorf("embedding text %d: blank text: %w", i, errs.ErrInvalidInput)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	batches := (len(texts) + o.cfg.BatchSize - 1) / o.cfg.BatchSize
	out := make([][]float32, 0, len(texts))
	for b := range batches {
		chunk := texts[b*o.cfg.BatchSize : min((b+1)*o.cfg.BatchSize, len(texts))]
		vecs, err := o.embedBatch(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("embedding batch %d/%d: %w", b+1, batches, err)
		}
		out = append(out, vecs...)
	}
	return out, nil
}

type ollamaRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (o *Ollama) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	payload, err := json.Marshal(ollamaRequest{Model: o.cfg.Model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, o.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, transportErr(ctx, "calling ollama", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, errorSnippetBytes))
		return nil, &StatusError{Code: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, o.cfg.MaxResponseBytes+1))
	if err != nil {
		return nil, transportErr(ctx, "reading response", err)
	}
	if int64(len(body)) > o.cfg.MaxResponseBytes {
		return nil, fmt.Errorf("response body too large (over %d bytes): %w",
			o.cfg.MaxResponseBytes, errs.ErrUpstream)
	}

	var parsed ollamaResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decoding response: %w: %w", errs.ErrUpstream, err)
	}
	if err := o.checkShape(parsed.Embeddings, len(texts)); err != nil {
		return nil, err
	}
	return parsed.Embeddings, nil
}

// checkShape verifies the count and that every vector has the client's dimension, recording
// that dimension on first success.
func (o *Ollama) checkShape(vecs [][]float32, want int) error {
	if len(vecs) != want {
		return fmt.Errorf("embedding count mismatch: sent %d texts, got %d vectors: %w",
			want, len(vecs), errs.ErrUpstream)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	dim := o.dim
	for i, v := range vecs {
		if len(v) == 0 {
			return fmt.Errorf("vector %d is empty: %w", i, errs.ErrUpstream)
		}
		if dim == 0 {
			dim = len(v)
		}
		if len(v) != dim {
			return fmt.Errorf("vector %d has dimension %d, want %d: %w", i, len(v), dim, errs.ErrUpstream)
		}
	}
	o.dim = dim
	return nil
}

// StatusError is a non-200 answer from Ollama, e.g. 404 when the model is not pulled. It wraps
// errs.ErrUpstream, so errors.Is(err, errs.ErrUpstream) still holds.
type StatusError struct {
	Code int
	Body string // first errorSnippetBytes bytes of the body, trimmed
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("ollama returned %d: %s: %v", e.Code, e.Body, errs.ErrUpstream)
}

func (e *StatusError) Unwrap() error { return errs.ErrUpstream }

// transportErr wraps a network error. A cancelled or expired context is reported as such, not
// as an upstream failure.
func transportErr(ctx context.Context, doing string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", doing, err)
	}
	return fmt.Errorf("%s: %w: %w", doing, errs.ErrUpstream, err)
}
