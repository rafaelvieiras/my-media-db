package scanner

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// blocoHash is the size of each block sampled in the partial content_hash: 1 MiB.
const blocoHash int64 = 1 << 20

// prefixoHash versions the content_hash scheme. If the scheme changes, the prefix
// changes with it and a re-scan recomputes without colliding with old hashes.
const prefixoHash = "p1:"

// contentHash computes the PARTIAL/SAMPLED content_hash (the locked decision of
// the hash scheme), stable across rename/move and fast on TB-sized files:
//
//	sha256( size(8 bytes LE) || head(1 MiB) || tail(1 MiB) ), with the "p1:" prefix.
//
// The `size` enters the hash to separate files that share the same blocks but have
// different sizes. Files with up to 2 blocks (<= 2 MiB) are read in full (head
// covers everything; there is no separate tail). Full-SHA was discarded
// (prohibitive I/O); path+mtime was discarded (it breaks on move).
func contentHash(f *os.File, size int64) (string, error) {
	h := sha256.New()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(size))
	h.Write(buf[:])

	if size <= 2*blocoHash {
		if _, err := io.Copy(h, f); err != nil {
			return "", fmt.Errorf("scanner: hash do arquivo inteiro: %w", err)
		}
		return prefixoHash + hex.EncodeToString(h.Sum(nil)), nil
	}

	// First block.
	if _, err := io.CopyN(h, f, blocoHash); err != nil {
		return "", fmt.Errorf("scanner: hash do bloco inicial: %w", err)
	}
	// Last block.
	if _, err := f.Seek(size-blocoHash, io.SeekStart); err != nil {
		return "", fmt.Errorf("scanner: seek para o bloco final: %w", err)
	}
	if _, err := io.CopyN(h, f, blocoHash); err != nil {
		return "", fmt.Errorf("scanner: hash do bloco final: %w", err)
	}
	return prefixoHash + hex.EncodeToString(h.Sum(nil)), nil
}
