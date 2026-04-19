package embedder

import "gonum.org/v1/gonum/floats"

// CosineSimilarity returns the cosine similarity between two vectors
// using gonum's BLAS-optimized Dot and Norm.
func CosineSimilarity(a, b []float64) float64 {
	denom := floats.Norm(a, 2) * floats.Norm(b, 2)
	if denom == 0 {
		return 0
	}
	return floats.Dot(a, b) / denom
}
