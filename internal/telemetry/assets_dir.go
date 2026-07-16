// assets_dir.go implementa TamanhoDiretorio (briefing 0063): o tamanho do
// cache interno da aplicação (assetsDir) para storage.app_cache_bytes de
// GET /health/system. Diferente de UsoDisco (que reaproveita o SUM já
// coletado pelo scanner da biblioteca), assetsDir é um diretório SÓ da
// aplicação -- pequeno comparado à biblioteca do usuário -- então um
// filepath.WalkDir simples e direto é suficiente, sem precisar de
// otimização incremental nem reaproveitar nenhum scan existente.
package telemetry

import (
	"io/fs"
	"os"
	"path/filepath"
)

// TamanhoDiretorio soma o tamanho de TODOS os arquivos regulares sob `dir`
// (recursivo). `dir` inexistente devolve (0, nil) -- assetsDir pode não ter
// sido criado ainda (instalação nova, sem nenhum asset baixado). Erros de
// leitura de um arquivo/subdiretório individual (ex.: removido entre o
// WalkDir listar e o Info) são ignorados (best-effort, mesmo espírito
// best-effort do resto deste pacote) -- só um erro na PRÓPRIA raiz (que não
// seja "não existe") aborta com erro.
func TamanhoDiretorio(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				if os.IsNotExist(err) {
					return filepath.SkipAll
				}
				return err
			}
			return nil // subitem ilegível: ignora, não aborta a soma.
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}
