package scanner

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// blocoHash é o tamanho de cada bloco amostrado no content_hash parcial: 1 MiB.
const blocoHash int64 = 1 << 20

// prefixoHash versiona o esquema do content_hash. Se o esquema mudar, o prefixo
// muda junto e um re-scan recalcula sem colidir com hashes antigos.
const prefixoHash = "p1:"

// contentHash calcula o content_hash PARCIAL/AMOSTRADO (decisão travada do
// briefing 0009 §6), estável a rename/move e rápido em arquivos de TB:
//
//	sha256( size(8 bytes LE) || head(1 MiB) || tail(1 MiB) ), com prefixo "p1:".
//
// O `size` entra no hash para separar arquivos que compartilhem os mesmos blocos
// mas tenham tamanhos diferentes. Arquivos com até 2 blocos (<= 2 MiB) são
// lidos por inteiro (head cobre tudo; não há tail separado). Full-SHA foi
// descartado (I/O proibitivo); path+mtime foi descartado (quebra no move).
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

	// Bloco inicial.
	if _, err := io.CopyN(h, f, blocoHash); err != nil {
		return "", fmt.Errorf("scanner: hash do bloco inicial: %w", err)
	}
	// Bloco final.
	if _, err := f.Seek(size-blocoHash, io.SeekStart); err != nil {
		return "", fmt.Errorf("scanner: seek para o bloco final: %w", err)
	}
	if _, err := io.CopyN(h, f, blocoHash); err != nil {
		return "", fmt.Errorf("scanner: hash do bloco final: %w", err)
	}
	return prefixoHash + hex.EncodeToString(h.Sum(nil)), nil
}
