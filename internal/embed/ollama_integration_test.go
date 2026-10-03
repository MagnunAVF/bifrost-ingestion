//go:build integration

package embed_test

import (
	"math"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/embed"
	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/fixture"
)

// TestOllamaIntegration runs against a real Ollama (`make e2e`). It fails, never skips, when
// Ollama is down: OLLAMA_HOST and BIFROST_MODEL override localhost and nomic-embed-text.
func TestOllamaIntegration(t *testing.T) {
	cfg := embed.OllamaConfig{BaseURL: os.Getenv("OLLAMA_HOST"), Model: os.Getenv("BIFROST_MODEL")}
	if cfg.Model == "" {
		cfg.Model = "nomic-embed-text"
	}
	o, err := embed.NewOllama(nil, cfg)
	require.NoError(t, err)
	cfg = o.Config()

	// Cold start: the first request loads the model.
	start := time.Now()
	_, err = o.Embed(t.Context(), []string{"warm-up"})
	require.NoError(t, err, "is Ollama running at %s with %q pulled?", cfg.BaseURL, cfg.Model)
	t.Logf("cold start: %v (model %s)", time.Since(start).Round(time.Millisecond), cfg.Model)

	identities := fixtureIdentities(t)
	start = time.Now()
	vecs, err := o.Embed(t.Context(), identities)
	require.NoError(t, err)
	elapsed := time.Since(start)
	t.Logf("warm: %d identities in %v (%.0f texts/s, batch size %d, dimension %d)",
		len(identities), elapsed.Round(time.Millisecond),
		float64(len(identities))/elapsed.Seconds(), cfg.BatchSize, len(vecs[0]))

	require.Len(t, vecs, len(identities))
	for i, v := range vecs {
		require.Len(t, v, len(vecs[0]), "vector %d", i)
	}

	// The fixture's real semantic duplicate (entry 57 vs Product 21) must beat an unrelated row.
	pair, err := o.Embed(t.Context(), []string{
		ingest.Identity("Roteador WiFi 6 TP-Link", "TP-Link", "Networking"),
		ingest.Identity("Router WiFi 6 TP-Link", "TP-Link", "Networking"),
		ingest.Identity("Roller Shades Blackout", "Achim Home", "Home Decor"),
	})
	require.NoError(t, err)
	same, other := cosine(pair[0], pair[1]), cosine(pair[0], pair[2])
	t.Logf("cosine: translation %.4f, unrelated %.4f", same, other)
	assert.Greater(t, same, other)
}

func fixtureIdentities(t *testing.T) []string {
	t.Helper()
	f, err := os.Open(fixture.Path(t, "ProductEntry.json"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	var out []string
	for rec, err := range ingest.Decode(t.Context(), f) {
		require.NoError(t, err)
		if !rec.Rejected() {
			out = append(out, rec.Identity)
		}
	}
	require.NotEmpty(t, out)
	return out
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
