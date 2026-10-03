# ENG-4: Local embedding engine integration (Ollama) (plan)

Status: approved 2026-10-03 (decisions 1-5 below). Plan 1.3, lane A, depends on ENG-1 (closed).
Branch: `feat/ENG-4-ollama-embedder`.

## Goal

Turn identity strings into embedding vectors with a local Ollama instance, through a small
`net/http` client, plus a deterministic fake that other packages' tests can use.

## Public API (internal/embed)

```go
// Embedder returns one vector per text, in input order; all vectors share one length.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type OllamaConfig struct {
	BaseURL          string        // default "http://localhost:11434"
	Model            string        // required, e.g. "nomic-embed-text"
	Timeout          time.Duration // per HTTP request; default 2m (covers the cold model load)
	BatchSize        int           // texts per POST; default 64
	MaxResponseBytes int64         // default 32 MiB
}

// NewOllama validates cfg (Model non-empty, BaseURL absolute http/https) and fills defaults.
// A nil client means a fresh &http.Client{} (no client-level timeout).
func NewOllama(client *http.Client, cfg OllamaConfig) (*Ollama, error)
func (o *Ollama) Config() OllamaConfig // effective config, defaults filled in
func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error)

// Fake is deterministic: FNV-1a(text) seeds a PRNG that fills Dim values in [-1,1).
func NewFake(dim int) *Fake // panics when dim <= 0
func (f *Fake) Embed(ctx context.Context, texts []string) ([][]float32, error)
```

- `POST {base}/api/embed` with `{"model":…, "input":[…]}`, chunked into `BatchSize` requests;
  results are concatenated in order.
- Each request gets its own `context.WithTimeout`, so a timeout satisfies
  `errors.Is(err, context.DeadlineExceeded)`.
- The body is read through `io.LimitReader(MaxResponseBytes+1)`; over the limit is an error. A
  non-200 includes up to 512 bytes of the body (Ollama's `{"error":"…"}`).
- The dimension of the first vector is remembered; any later vector of another length fails.
- New sentinel `errs.ErrUpstream` for non-200, malformed or oversized bodies, count mismatch and
  inconsistent dimensions. Blank input text and invalid config wrap `errs.ErrInvalidInput`.

## Tests

- `TestOllamaEmbed` (httptest.Server): happy path (order, POST, path, content type, body);
  batching 130 → 64/64/2; empty input sends nothing; blank text → ErrInvalidInput; 500/404 with
  Ollama's error body; malformed JSON; count mismatch; mixed dimensions in one response;
  dimension change across calls; empty vector; oversized body; request timeout; caller ctx
  cancelled mid-flight; ctx cancelled before the call; failure in a later batch.
- `TestNewOllama`: missing Model, relative / non-http BaseURL, negative BatchSize or Timeout →
  ErrInvalidInput; defaults filled in.
- `TestFake`: deterministic across calls and instances, different texts differ, length Dim, no
  NaN, never zero, empty input, cancelled ctx, `NewFake(0)` panics; compile-time `Embedder`
  checks for `*Fake` and `*Ollama`.
- `TestOllamaIntegration` (`//go:build integration`, `OLLAMA_HOST` / `BIFROST_MODEL`): embeds
  every valid fixture identity via `ingest.Decode`, checks count and dimension, logs cold-start
  latency and warm throughput, and checks that `Roteador WiFi 6 TP-Link` is closer to `Router
  WiFi 6 TP-Link` than to an unrelated product. Fails (does not skip) when Ollama is down.

## Tasks (one commit each)

1. `Embedder` and `Fake`.
2. Ollama client and `errs.ErrUpstream`.
3. Integration test with throughput measurement; this plan and the M1 Decisions.

## Decisions (approved 2026-10-03)

1. The client batches internally, 64 texts per request by default.
2. One per-request timeout, default 2 minutes, which covers the cold model load.
3. The integration test fails, not skips, when Ollama is unreachable.
4. `NewFake(dim <= 0)` panics (test helper).
5. No nomic task prefixes (`search_document:` / `clustering:`) in the embedder; whether to add
   them belongs to the ENG-6 calibration, since it changes the identity contract.
