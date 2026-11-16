package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hashDePath is a test shorthand: it opens the file and computes the content_hash.
func hashDePath(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	h, err := hashDoArquivo(path, info.Size())
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return h
}

// TestContentHashEstavelEDistinto proves the two properties of the content_hash:
// the same content (even at another path — rename/move) gives the SAME hash;
// different content gives a different hash. And the scheme prefix is present.
func TestContentHashEstavelEDistinto(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mkv")
	b := filepath.Join(dir, "b.mkv") // same content as a, another name (move)
	c := filepath.Join(dir, "c.mkv") // different content

	conteudo := []byte("conteudo de video sintetico xyz")
	if err := os.WriteFile(a, conteudo, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, conteudo, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("outro conteudo"), 0o644); err != nil {
		t.Fatal(err)
	}

	ha, hb, hc := hashDePath(t, a), hashDePath(t, b), hashDePath(t, c)
	if ha != hb {
		t.Errorf("hash instável a rename: %q != %q", ha, hb)
	}
	if ha == hc {
		t.Errorf("hashes de conteúdos diferentes coincidiram: %q", ha)
	}
	if !strings.HasPrefix(ha, prefixoHash) {
		t.Errorf("hash sem o prefixo do esquema: %q", ha)
	}
}

// TestContentHashArquivoGrande exercises the head+tail path (> 2 blocks): two files
// with the same beginning and end but a different MIDDLE only have different hashes
// if the size changes; with the same size, the partial hash may coincide (it is the
// documented trade-off). Here we guarantee that different sizes separate them.
func TestContentHashArquivoGrande(t *testing.T) {
	dir := t.TempDir()
	grande := filepath.Join(dir, "grande.mkv")
	maior := filepath.Join(dir, "maior.mkv")

	buf := make([]byte, 2*blocoHash+1024) // > 2 blocks: uses head+tail
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	if err := os.WriteFile(grande, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(maior, append(buf, 0xFF), 0o644); err != nil {
		t.Fatal(err)
	}
	if hashDePath(t, grande) == hashDePath(t, maior) {
		t.Error("hashes de tamanhos diferentes coincidiram (size deveria entrar no hash)")
	}
}
