package packsync

import (
	"context"
	"io"
	"os"
)

// CurseForge uses MurmurHash2 (seed 1) after removing these four byte values.
// Count during the normal SHA pass and hash the frozen snapshot in a second
// streaming pass, without holding a large JAR in memory.
func curseSpace(b byte) bool { return b == 9 || b == 10 || b == 13 || b == 32 }

type curseByteCounter struct{ n uint32 }

func (c *curseByteCounter) Write(data []byte) (int, error) {
	for _, b := range data {
		if !curseSpace(b) {
			c.n++
		}
	}
	return len(data), nil
}

type curseHasher struct {
	value uint32
	tail  uint32
	used  uint
}

func (h *curseHasher) Write(data []byte) (int, error) {
	for _, b := range data {
		if curseSpace(b) {
			continue
		}
		h.tail |= uint32(b) << (8 * h.used)
		h.used++
		if h.used == 4 {
			k := h.tail
			k *= 0x5bd1e995
			k ^= k >> 24
			k *= 0x5bd1e995
			h.value = (h.value * 0x5bd1e995) ^ k
			h.tail = 0
			h.used = 0
		}
	}
	return len(data), nil
}
func (h *curseHasher) sum() uint32 {
	v := h.value
	if h.used > 0 {
		v ^= h.tail
		v *= 0x5bd1e995
	}
	v ^= v >> 13
	v *= 0x5bd1e995
	v ^= v >> 15
	return v
}

type contextInput struct {
	ctx context.Context
	io.Reader
}

func (r contextInput) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(b)
}
func curseFileFingerprint(ctx context.Context, path string, count uint32) (uint32, error) {
	input, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer input.Close()
	hasher := curseHasher{value: 1 ^ count}
	if _, err = io.Copy(&hasher, contextInput{ctx, input}); err != nil {
		return 0, err
	}
	return hasher.sum(), nil
}
