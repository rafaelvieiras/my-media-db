package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hashDePath é um atalho de teste: abre o arquivo e calcula o content_hash.
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

// TestContentHashEstavelEDistinto prova as duas propriedades do content_hash: o
// mesmo conteúdo (mesmo em outro caminho — rename/move) dá o MESMO hash; conteúdo
// diferente dá hash diferente. E o prefixo do esquema está presente.
func TestContentHashEstavelEDistinto(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mkv")
	b := filepath.Join(dir, "b.mkv") // mesmo conteúdo de a, outro nome (move)
	c := filepath.Join(dir, "c.mkv") // conteúdo diferente

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

// TestContentHashArquivoGrande exercita o caminho head+tail (> 2 blocos): dois
// arquivos com o mesmo início e fim mas MEIO diferente têm hashes diferentes só
// se o tamanho mudar; com o mesmo tamanho, o hash parcial pode coincidir (é o
// trade-off documentado). Aqui garantimos que tamanhos diferentes separam.
func TestContentHashArquivoGrande(t *testing.T) {
	dir := t.TempDir()
	grande := filepath.Join(dir, "grande.mkv")
	maior := filepath.Join(dir, "maior.mkv")

	buf := make([]byte, 2*blocoHash+1024) // > 2 blocos: usa head+tail
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
