package server

// midias_faltantes.go implementa a materialização "sob demanda" reusada pelo
// job midias_faltantes (briefing 0064, internal/job/midiasfaltantes): para
// UMA entity, materializa fisicamente na pasta REAL da biblioteca a arte HOJE
// VENCEDORA (mesmo agregador da galeria, montarGruposArtwork/artwork.go,
// 0048) de cada kind relevante, e o .nfo quando ausente (mesma função
// gravarNFO do 0062, localmedia_handlers.go). Nunca escolhe um candidato
// diferente do que a UI já mostraria como topo -- só aplica no disco o que já
// é a escolha vigente (briefing 0064 §3, explicitamente FORA de escopo:
// auto-selecionar quando NADA foi resolvido ainda).

import (
	"context"
	"errors"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// kindsMidiasFaltantes são os kinds de arte de NÍVEL DE OBRA que o job
// materializa (briefing 0064 §4 cita "poster, fanart, banner, clearlogo,
// discart, landscape" -- os três últimos usam a NOMENCLATURA DE ARQUIVO do
// internal/localmedia (KindClearlogo/KindDiscart/KindLandscape), não o
// vocabulário de asset.kind gravado no banco e agrupado por
// montarGruposArtwork (assets.go/fanarttv.go: poster, fanart, banner, logo,
// clearart, disc, thumb, characterart -- "clearlogo"/"discart"/"landscape"
// NUNCA aparecem como chave desse agregador). Decisão de implementação (não
// 100% explícita no briefing, resolvendo essa divergência de vocabulário):
// usamos aqui o vocabulário REAL do agregador, mapeando pela intenção --
// poster, fanart, banner, logo (equivalente de "clearlogo" na pasta real),
// disc (equivalente de "discart") e thumb (arte 16:9 de nível de série/filme,
// o equivalente mais próximo de "landscape" no vocabulário existente:
// kindsTV mapeia "tvthumb"->"thumb" na fanart.tv). `still` fica de fora --
// é SEMPRE por EPISÓDIO (briefing 0044), fora do escopo deste job de nível
// de obra.
var kindsMidiasFaltantes = []string{kindPoster, kindFanart, kindBanner, kindLogo, kindDisc, kindThumb}

// ListarEntitiesComMediaFilePresente devolve os ids de TODA entity que possui
// ao menos um media_file 'present' -- o universo elegível do job
// midias_faltantes (briefing 0064). Para série, media_file.entity_id aponta
// pra SÉRIE (nunca season/episódio, ver internal/scanner/serie.go
// gravarArquivoSerie) -- então season/episódio nunca aparecem aqui, o que já
// casa com a guarda de writeEntityNfo (season não tem NFO próprio).
func (s *Server) ListarEntitiesComMediaFilePresente(ctx context.Context) ([]string, error) {
	return s.leitura().ListarEntityIDsComMediaFilePresente(ctx)
}

// MaterializarMidiasFaltantes materializa, para UMA entity, a arte vencedora
// (agregador montarGruposArtwork) de cada kind de kindsMidiasFaltantes ainda
// sem local_path, e o NFO quando entity.nfo_path é nulo -- o corpo do job
// midias_faltantes (briefing 0064), também chamável isoladamente (não-HTTP,
// é isso que internal/job/midiasfaltantes.Materializador consome). Erro só
// propaga em falha real de LEITURA da entity (banco fora do ar); falhas
// pontuais de download/materialização de UM kind ou do NFO são logadas e
// puladas (best-effort -- mesmo espírito do resto do sistema de jobs,
// 0053/0054/0056), nunca abortam as demais.
func (s *Server) MaterializarMidiasFaltantes(ctx context.Context, entityID string) (artesBaixadas []string, nfoGravado bool, err error) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return nil, false, err
	}

	for _, kind := range kindsMidiasFaltantes {
		ok, kerr := s.materializarKindVencedor(ctx, ent, kind)
		if kerr != nil {
			log.Printf("midias_faltantes: falha ao materializar o kind %q da entity %q (best-effort, ignorado): %v", kind, entityID, kerr)
			continue
		}
		if ok {
			artesBaixadas = append(artesBaixadas, kind)
		}
	}

	nfoGravado, nerr := s.gravarNFOSeFaltante(ctx, ent)
	if nerr != nil {
		log.Printf("midias_faltantes: falha ao gravar o NFO da entity %q (best-effort, ignorado): %v", entityID, nerr)
	}
	return artesBaixadas, nfoGravado, nil
}

// materializarKindVencedor pega o PRIMEIRO item do agregador
// (montarGruposArtwork, MESMA precedência da galeria/0048/0062 --
// ordenarGruposArtwork já ordena is_principal desc, is_local_in_library desc)
// para `kind` e materializa fisicamente na pasta real quando ainda faltava --
// nunca escolhe um candidato diferente do que a UI já mostraria como topo.
// Devolve ok=false sem erro quando não há candidato para o kind OU o
// vencedor já está materializado (local_path preenchido) -- ambos "nada a
// fazer", não é falha.
func (s *Server) materializarKindVencedor(ctx context.Context, ent sqlcgen.Entity, kind string) (bool, error) {
	grupos := s.montarGruposArtwork(ctx, ent.ID)
	candidatos := grupos[kind]
	if len(candidatos) == 0 {
		return false, nil
	}
	vencedor := candidatos[0]

	if vencedor.IsLocal {
		if vencedor.IsLocalInLibrary {
			return false, nil // já materializado, nada a fazer
		}
		asset, err := s.leitura().ObterAsset(ctx, vencedor.AssetID)
		if err != nil {
			return false, err
		}
		if _, err := s.materializarAsset(ctx, ent, asset); err != nil {
			return false, err
		}
		return true, nil
	}

	if vencedor.SrcURL == "" {
		return false, nil // sem URL de origem conhecida -- nada a baixar
	}
	asset, err := s.baixarEMaterializarArte(ctx, ent.ID, vencedor.SrcURL, kind)
	if err != nil {
		if errors.Is(err, errBaixarArte) {
			// Falha de DOWNLOAD (rede/status/tipo) -- best-effort, não é erro
			// de infra (mesmo tratamento de baixarMelhorArteDoKind, 0059).
			return false, nil
		}
		return false, err
	}
	if _, err := s.materializarAsset(ctx, ent, asset); err != nil {
		return false, err
	}
	return true, nil
}

// gravarNFOSeFaltante gera o NFO da entity (mesma função gravarNFO do 0062)
// quando ainda não existe (entity.nfo_path nulo). season nunca tem NFO
// próprio (mesma guarda de writeEntityNfo) -- defensivo aqui, já que season
// nunca aparece no universo de ListarEntitiesComMediaFilePresente de
// qualquer forma. Erro só propaga em falha real (banco/disco);
// "sem pasta de destino" (errSemDestino -- entity sem media_file present,
// nunca deveria acontecer aqui, mas defensivo) degrada para (false, nil).
func (s *Server) gravarNFOSeFaltante(ctx context.Context, ent sqlcgen.Entity) (bool, error) {
	if ent.NfoPath.Valid && ent.NfoPath.String != "" {
		return false, nil
	}
	if ent.Type == tipoEntTemporada {
		return false, nil
	}

	resolvida, encontrada, err := s.resolverEntity(ctx, ent.ID, "", false)
	if err != nil {
		return false, err
	}
	if !encontrada {
		return false, nil
	}
	if _, err := s.gravarNFO(ctx, ent, resolvida); err != nil {
		if errors.Is(err, errSemDestino) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
