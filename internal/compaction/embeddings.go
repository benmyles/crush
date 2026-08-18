package compaction

import (
	"context"
	"database/sql"
	"encoding/binary"
	"math"
	"sort"

	"github.com/charmbracelet/crush/internal/db"
)

// EmbeddingModel computes an embedding vector for a text. Implementations wrap
// a fantasy embedding model; the engine stores vectors in compaction_embeddings.
type EmbeddingModel func(ctx context.Context, text string) ([]float32, error)

// IndexEmbeddings computes and persists embeddings for the given message ids +
// texts. It is optional (gated by CompactionConfig.Embeddings) and provides the
// dense retrieval pathway that AgentMemBench showed is the only mechanism that
// scales to long-range recall.
func (e *Engine) IndexEmbeddings(ctx context.Context, sessionID, summaryID string, items []EmbeddingItem, model EmbeddingModel) error {
	if model == nil {
		return nil
	}
	for _, item := range items {
		vec, err := model(ctx, item.Text)
		if err != nil {
			continue
		}
		blob := encodeFloat32s(vec)
		_ = e.q.CreateCompactionEmbedding(ctx, db.CreateCompactionEmbeddingParams{
			MessageID: item.MessageID,
			SummaryID: sql.NullString{String: summaryID, Valid: summaryID != ""},
			SessionID: sessionID,
			Embedding: blob,
			CreatedAt: e.now(),
		})
	}
	return nil
}

// EmbeddingItem is one message to embed.
type EmbeddingItem struct {
	MessageID string
	Text      string
}

// SearchEmbeddings performs cosine-similarity retrieval over the session's
// stored embeddings, returning the top-k message ids. This backs recall_query.
func (e *Engine) SearchEmbeddings(ctx context.Context, sessionID string, queryVec []float32, k int) ([]EmbeddingHit, error) {
	if k <= 0 {
		k = 5
	}
	rows, err := e.q.ListCompactionEmbeddingsBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var hits []EmbeddingHit
	for _, row := range rows {
		vec, ok := decodeFloat32s(row.Embedding)
		if !ok {
			continue
		}
		score := cosine(queryVec, vec)
		hits = append(hits, EmbeddingHit{MessageID: row.MessageID, Score: score})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

// EmbeddingHit is one retrieval result.
type EmbeddingHit struct {
	MessageID string
	Score     float32
}

func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(na))*math.Sqrt(float64(nb)))
}

func encodeFloat32s(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeFloat32s(b []byte) ([]float32, bool) {
	if len(b)%4 != 0 || len(b) == 0 {
		return nil, false
	}
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, true
}

// encodeEmbeddingParams is kept for testability; it builds the sqlc params
// with the sql.NullString handling isolated from callers.
func encodeEmbeddingParams(sessionID, summaryID, messageID string, blob []byte, now int64) db.CreateCompactionEmbeddingParams {
	return db.CreateCompactionEmbeddingParams{
		MessageID: messageID,
		SummaryID: sql.NullString{String: summaryID, Valid: summaryID != ""},
		SessionID: sessionID,
		Embedding: blob,
		CreatedAt: now,
	}
}
