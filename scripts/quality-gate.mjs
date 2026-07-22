#!/usr/bin/env node
/**
 * quality-gate.mjs — Catraca (ratchet) de qualidade unificada do MyMediaDB.
 *
 * Baseado no gate standalone gerado pelo boatman (github.com/rafaelvieiras/boatman):
 * reaproveita o schema do baseline, os modos de CLI, a lógica de ratchet e o
 * render (humano/Markdown). A diferença é que esta versão é POLIGLOTA e mora na
 * RAIZ do monorepo: coleta métricas de Go (rodadas na raiz) E de TypeScript
 * (rodadas em `web/`) num único `baseline.json`.
 *
 * Ao contrário do gate original (que apenas LÊ arquivos de relatório já gerados
 * por passos externos), este gate RODA os coletores ele mesmo em todos os modos.
 * Assim o `--local` é autocontido: o `pre-push` só precisa chamar este script.
 *
 * Zero dependências além dos built-ins do Node.
 *
 * Uso:
 *   node scripts/quality-gate.mjs                    # modo CI (saída Markdown)
 *   node scripts/quality-gate.mjs --local            # saída humana (terminal)
 *   node scripts/quality-gate.mjs --generate-baseline# roda coletores e grava baseline.json
 *   node scripts/quality-gate.mjs --local --fast     # filtro por diff + só coletores baratos
 *   node scripts/quality-gate.mjs --local --full     # ignora o filtro por diff, roda tudo
 *
 * Filtro por diff (briefing 0061): `--local`/CI só rodam os coletores do lado
 * (Go/Web) que o diff atual realmente tocou (ver getChangedFiles/classifyDiff
 * abaixo); `--generate-baseline` e `--full` sempre ignoram esse filtro.
 *
 * Robustez: ferramentas Go OPCIONAIS (golangci-lint, govulncheck, gocyclo) que
 * não estejam no PATH fazem a métrica ser PULADA (com aviso), nunca quebram nem
 * bloqueiam o gate. gofmt e `go test` vêm com o Go; jscpd e npm vêm do web.
 */

import { existsSync, readFileSync, writeFileSync, mkdirSync, rmSync } from 'node:fs';
import { execSync, spawnSync } from 'node:child_process';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = join(__dirname, '..');
const WEB = join(ROOT, 'web');
const REPORTS = join(ROOT, 'reports');

const BASELINE_PATH = join(ROOT, 'baseline.json');
const PROJECT = 'my-media-db';

const isFast          = process.argv.includes('--fast');
const isFull          = process.argv.includes('--full');
// --fast sempre imprime no formato humano (é modo de iteração interativa),
// mesmo que alguém rode só `--fast` sem `--local` explícito.
const isLocal        = process.argv.includes('--local') || isFast;
const isGenBaseline  = process.argv.includes('--generate-baseline');
const isE2E          = process.argv.includes('--e2e');
const isCI           = !isLocal && !isGenBaseline;

// ─── ANSI helpers (só quando --local) ────────────────────────────────────────
const c = {
  reset:  isLocal ? '\x1b[0m'  : '',
  bold:   isLocal ? '\x1b[1m'  : '',
  dim:    isLocal ? '\x1b[2m'  : '',
  green:  isLocal ? '\x1b[32m' : '',
  yellow: isLocal ? '\x1b[33m' : '',
  red:    isLocal ? '\x1b[31m' : '',
  cyan:   isLocal ? '\x1b[36m' : '',
};

function log(msg)  { console.log(msg); }
function warn(msg) { console.warn(`${c.yellow}⚠  ${msg}${c.reset}`); }

// ─── Definição das métricas ──────────────────────────────────────────────────
/**
 * lowerIsBetter: true  → regressão quando atual > baseline
 * lowerIsBetter: false → regressão quando atual < baseline
 * blocking: true       → sai 1 em regressão (pior que o baseline)
 * blocking: 'zero'     → sai 1 se atual > 0 (deve permanecer 0)
 * blocking: false      → só avisa
 * side: 'go' | 'web'   → apenas rótulo de agrupamento no relatório
 */
const METRIC_DEFS = {
  // --- Go (rodado na raiz) ---
  go_coverage_lines:     { label: 'Go: cobertura de linhas', unit: '%',     side: 'go',  lowerIsBetter: false, blocking: true   },
  e2e_coverage_lines:    { label: 'Go: cobertura E2E (jornadas)', unit: '%',side: 'go',  lowerIsBetter: false, blocking: true   },
  go_gofmt_unformatted:  { label: 'Go: arquivos não-gofmt',  unit: 'arqs',  side: 'go',  lowerIsBetter: true,  blocking: 'zero' },
  go_lint_issues:        { label: 'Go: issues golangci-lint',unit: 'issues',side: 'go',  lowerIsBetter: true,  blocking: false  },
  go_vuln_high:          { label: 'Go: vulnerabilidades',    unit: 'vulns', side: 'go',  lowerIsBetter: true,  blocking: 'zero' },
  go_cyclo_violations:   { label: 'Go: funções > gocyclo 10',unit: 'funcs', side: 'go',  lowerIsBetter: true,  blocking: false  },
  // --- TypeScript / web (rodado em web/) ---
  web_coverage_lines:    { label: 'Web: cobertura de linhas',unit: '%',     side: 'web', lowerIsBetter: false, blocking: true   },
  web_typecheck_errors:  { label: 'Web: erros de type-check',unit: 'erros', side: 'web', lowerIsBetter: true,  blocking: 'zero' },
  web_duplicate_percent: { label: 'Web: duplicação',         unit: '%',     side: 'web', lowerIsBetter: true,  blocking: true   },
  web_lint_issues:       { label: 'Web: issues ESLint',      unit: 'issues',side: 'web', lowerIsBetter: true,  blocking: false  },
  web_audit_critical:    { label: 'Web: vulns critical',     unit: 'vulns', side: 'web', lowerIsBetter: true,  blocking: 'zero' },
  // --- E2E navegador (Camada 2, briefing 0068) — lido de artefatos do `make e2e-cov` ---
  e2e_binary_coverage_lines: { label: 'E2E: cobertura do binário (navegador)', unit: '%', side: 'e2e', lowerIsBetter: false, blocking: true },
  e2e_front_coverage_lines:  { label: 'E2E: cobertura do front (navegador)',   unit: '%', side: 'e2e', lowerIsBetter: false, blocking: true },
};

// ─── Filtro por diff (briefing 0061) ─────────────────────────────────────────
// Arquivos de "infra do próprio gate": qualquer mudança neles força rodada
// completa dos dois lados, pra nunca criar um ponto cego onde uma mudança no
// gate/CI escapa de validação.
const GATE_INFRA_FILES = new Set([
  'scripts/quality-gate.mjs',
  'lefthook.yml',
  '.golangci.yml',
  'baseline.json',
  'Makefile',
]);
function isGateInfraFile(f) {
  return GATE_INFRA_FILES.has(f) || f.startsWith('.github/workflows/');
}

const GO_SIDE_RE  = /^(cmd\/|internal\/|go\.mod$|go\.sum$)/;
const WEB_SIDE_RE = /^web\//;

/**
 * Devolve a lista de arquivos mudados (`string[]`) ou `null` quando não foi
 * possível determinar a base do diff com segurança. `null` é SEMPRE tratado
 * como "rode tudo" por quem chama — nunca o contrário.
 */
function getChangedFiles() {
  if (isCI) {
    const base = process.env.GATE_DIFF_BASE;
    if (!base) return null;
    const check = spawnSync('git', ['cat-file', '-e', base], { cwd: ROOT, stdio: 'ignore' });
    if (check.status !== 0) return null;
    const r = run(`git diff --name-only ${base}...HEAD`);
    if (r.code !== 0) return null;
    return r.stdout.split('\n').map((l) => l.trim()).filter(Boolean);
  }

  // Local: tenta upstream → origin/master → origin/main, nesta ordem. Diff
  // contra a ÁRVORE DE TRABALHO (não só HEAD) de propósito: `make gate-fast`
  // é pra ser usado DURANTE a implementação, antes de commitar (ver §2 do
  // briefing 0061) — se comparasse só commits, mudanças ainda não commitadas
  // (o caso comum de quem está iterando) ficariam invisíveis pro filtro.
  const candidateBases = ['@{upstream}', 'origin/master', 'origin/main'];
  for (const ref of candidateBases) {
    const mb = run(`git merge-base HEAD "${ref}"`);
    if (mb.code !== 0 || !mb.stdout.trim()) continue;
    const mergeBase = mb.stdout.trim();
    const diff = run(`git diff --name-only ${mergeBase}`);
    if (diff.code === 0) {
      return diff.stdout.split('\n').map((l) => l.trim()).filter(Boolean);
    }
  }
  return null;
}

/**
 * Classifica a lista de arquivos mudados em `{ touchesGo, touchesWeb }`.
 * `files === null` (base indeterminada) sempre vira `{ true, true }`.
 */
function classifyDiff(files) {
  if (files === null) return { touchesGo: true, touchesWeb: true };

  let touchesGo = false;
  let touchesWeb = false;

  for (const f of files) {
    if (isGateInfraFile(f)) { touchesGo = true; touchesWeb = true; break; }
  }
  if (!(touchesGo && touchesWeb)) {
    for (const f of files) {
      if (GO_SIDE_RE.test(f)) touchesGo = true;
      if (WEB_SIDE_RE.test(f)) touchesWeb = true;
    }
  }
  return { touchesGo, touchesWeb };
}

// ─── Helpers de execução ─────────────────────────────────────────────────────
function commandExists(cmd) {
  // Comando único como string (sem array de args) para evitar o DEP0190 do
  // Node ao combinar `shell: true` com argumentos.
  const probe = process.platform === 'win32' ? `where ${cmd}` : `command -v ${cmd}`;
  const r = spawnSync(probe, { stdio: 'ignore', shell: true });
  return r.status === 0;
}

/**
 * Roda um comando e devolve { code, stdout, stderr }. Nunca lança: captura
 * exit codes não-zero (várias ferramentas saem !=0 quando "acham algo").
 */
function run(cmd, opts = {}) {
  try {
    const stdout = execSync(cmd, {
      cwd: opts.cwd ?? ROOT,
      stdio: ['ignore', 'pipe', 'pipe'],
      encoding: 'utf8',
      shell: true,
      maxBuffer: 64 * 1024 * 1024,
      env: process.env,
    });
    return { code: 0, stdout, stderr: '' };
  } catch (e) {
    return {
      code: typeof e.status === 'number' ? e.status : 1,
      stdout: e.stdout ? e.stdout.toString() : '',
      stderr: e.stderr ? e.stderr.toString() : '',
    };
  }
}

function countNonEmptyLines(s) {
  return s.split('\n').filter((l) => l.trim() !== '').length;
}

// Log completo sob demanda (briefing 0061 §4.4): quando um coletor falha ao
// RODAR (não "achou issues" — não conseguiu produzir resultado), grava
// stdout+stderr completos em reports/<chave>-failure.log e mostra só as
// últimas ~40 linhas no terminal/Markdown — o `--- FAIL:` real de go test/
// vitest costuma ir pro stdout, que antes era descartado silenciosamente.
function logFailure(key, stdout, stderr, message) {
  const combined = `${stdout ?? ''}\n${stderr ?? ''}`;
  mkdirSync(REPORTS, { recursive: true });
  const logPath = join(REPORTS, `${key}-failure.log`);
  writeFileSync(logPath, combined, 'utf8');
  const tail = combined.split('\n').slice(-40).join('\n').trim();
  warn(message);
  if (tail) warn(tail);
  warn(`Saída completa em: ${logPath}`);
}

// ─── Coletores Go (rodados na raiz) ──────────────────────────────────────────

// Cobertura: go test -coverprofile → go tool cover -func → total %.
// go test/gofmt sempre existem (vêm com o Go). Se os testes falharem em
// compilar/rodar, avisamos e a métrica vira null (pulada) — nunca crasha.
function collectGoCoverage() {
  const profile = join(REPORTS, 'go-coverage.out');
  const t = run(`go test ./... -coverprofile="${profile}" -covermode=set`);
  if (t.code !== 0) {
    logFailure('go_coverage_lines', t.stdout, t.stderr, 'go test falhou — cobertura não medida (métrica pulada):');
    return null;
  }
  if (!existsSync(profile)) {
    warn('perfil de cobertura não gerado — métrica pulada');
    return null;
  }
  const f = run(`go tool cover -func="${profile}"`);
  if (f.code !== 0) {
    warn('go tool cover falhou — cobertura pulada');
    return null;
  }
  const line = f.stdout.trim().split('\n').filter((l) => l.startsWith('total:')).pop();
  const m = line && line.match(/(\d+(?:\.\d+)?)%/);
  return m ? parseFloat(m[1]) : 0;
}

// Cobertura E2E (briefing 0028): quanto do código do APP as JORNADAS da Camada 1
// (internal/e2e) tocam, medido com -coverpkg sobre o resto de internal/ MENOS a infra
// de teste (o próprio e2e e o provider fake, que inflariam o número). Métrica SEPARADA
// do unitário de propósito (jornada × ramo). Roda com -count=1 (o cache do go test já
// mascarou falha antes). null quando pula (nunca crasha).
function collectE2ECoverage() {
  const listed = run('go list ./internal/...');
  if (listed.code !== 0) {
    warn('go list falhou — cobertura E2E pulada');
    return null;
  }
  const pkgs = listed.stdout
    .trim()
    .split('\n')
    .filter((p) => p && !p.endsWith('/internal/e2e') && !p.endsWith('/internal/provider/fake'));
  if (pkgs.length === 0) {
    warn('nenhum pacote para -coverpkg — cobertura E2E pulada');
    return null;
  }
  const profile = join(REPORTS, 'e2e-coverage.out');
  const t = run(
    `go test ./internal/e2e/... -coverpkg="${pkgs.join(',')}" -covermode=set -coverprofile="${profile}" -count=1`,
  );
  if (t.code !== 0) {
    logFailure('e2e_coverage_lines', t.stdout, t.stderr, 'go test e2e falhou — cobertura E2E não medida (métrica pulada):');
    return null;
  }
  if (!existsSync(profile)) {
    warn('perfil E2E não gerado — métrica pulada');
    return null;
  }
  const f = run(`go tool cover -func="${profile}"`);
  if (f.code !== 0) {
    warn('go tool cover (e2e) falhou — cobertura E2E pulada');
    return null;
  }
  const line = f.stdout.trim().split('\n').filter((l) => l.startsWith('total:')).pop();
  const m = line && line.match(/(\d+(?:\.\d+)?)%/);
  return m ? parseFloat(m[1]) : 0;
}

// gofmt -l: nº de arquivos .go não formatados. Sempre disponível.
function collectGofmt() {
  // Restringe a diretórios de código Go para não caminhar node_modules.
  const r = run('gofmt -l cmd internal');
  if (r.code !== 0) {
    warn('gofmt -l retornou erro: ' + (r.stderr.trim() || 'desconhecido'));
  }
  return countNonEmptyLines(r.stdout);
}

// golangci-lint (OPCIONAL): conta Issues do JSON. v2 usa --output.json.path.
function collectGolangciLint() {
  if (!commandExists('golangci-lint')) {
    warn('golangci-lint não está no PATH — métrica "go_lint_issues" pulada (instale com: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest)');
    return null;
  }
  const out = join(REPORTS, 'golangci.json');
  // Escrevemos o JSON num arquivo (não em stdout) porque o golangci-lint v2
  // imprime também um resumo humano ("N issues.") em stdout, que sujaria o JSON.
  const r = run(`golangci-lint run --config .golangci.yml --output.json.path "${out}" ./...`);
  // exit 1 = achou issues (esperado); só falha real se o arquivo não existir.
  if (!existsSync(out)) {
    logFailure('go_lint_issues', r.stdout, r.stderr, 'golangci-lint não produziu relatório JSON — métrica pulada:');
    return null;
  }
  try {
    const data = JSON.parse(readFileSync(out, 'utf8'));
    return Array.isArray(data.Issues) ? data.Issues.length : 0;
  } catch (e) {
    warn('não foi possível parsear o JSON do golangci-lint: ' + e.message);
    return null;
  }
}

// govulncheck (OPCIONAL): stream de objetos JSON. Contamos OSVs distintos com
// ao menos uma "finding" alcançável (frame de topo com função) — o mesmo que o
// govulncheck reporta como "your code is affected by N vulnerabilities".
function collectGovulncheck() {
  if (!commandExists('govulncheck')) {
    warn('govulncheck não está no PATH — métrica "go_vuln_high" pulada (instale com: go install golang.org/x/vuln/cmd/govulncheck@latest)');
    return null;
  }
  const r = run('govulncheck -json ./...');
  if (r.code !== 0 && !r.stdout.trim()) {
    logFailure('go_vuln_high', r.stdout, r.stderr, 'govulncheck falhou sem saída — métrica pulada:');
    return null;
  }
  const affected = new Set();
  for (const obj of splitJsonStream(r.stdout)) {
    const f = obj.finding;
    if (!f || !Array.isArray(f.trace) || f.trace.length === 0) continue;
    const top = f.trace[0];
    // "alcançável" = o frame de topo tem função (o código realmente chama).
    if (top && top.function) affected.add(f.osv);
  }
  return affected.size;
}

// gocyclo (OPCIONAL): nº de funções acima do limite 10.
function collectGocyclo() {
  if (!commandExists('gocyclo')) {
    warn('gocyclo não está no PATH — métrica "go_cyclo_violations" pulada (instale com: go install github.com/fzipp/gocyclo/cmd/gocyclo@latest)');
    return null;
  }
  const r = run('gocyclo -over 10 cmd internal');
  // gocyclo sai 1 quando há violações; a saída (uma linha por função) é o que conta.
  return countNonEmptyLines(r.stdout);
}

// Divide um stream de objetos JSON concatenados (como o -json do govulncheck)
// em objetos individuais, casando chaves { } no nível superior.
function splitJsonStream(raw) {
  const out = [];
  let depth = 0, start = -1, inStr = false, esc = false;
  for (let i = 0; i < raw.length; i++) {
    const ch = raw[i];
    if (inStr) {
      if (esc) esc = false;
      else if (ch === '\\') esc = true;
      else if (ch === '"') inStr = false;
      continue;
    }
    if (ch === '"') { inStr = true; continue; }
    if (ch === '{') { if (depth === 0) start = i; depth++; }
    else if (ch === '}') {
      depth--;
      if (depth === 0 && start >= 0) {
        try { out.push(JSON.parse(raw.slice(start, i + 1))); } catch { /* ignora fragmentos */ }
        start = -1;
      }
    }
  }
  return out;
}

// ─── Coletores Web (rodados em web/) ─────────────────────────────────────────

// jscpd sobre web/src → percentual de duplicação.
function collectWebDuplication() {
  if (!existsSync(WEB)) { warn('web/ não encontrado — duplicação pulada'); return null; }
  const outDir = join(REPORTS, 'jscpd');
  mkdirSync(outDir, { recursive: true });
  // Roda em web/, aponta a saída para reports/jscpd na raiz.
  const r = run(`npx --yes jscpd src --reporters json --output "${outDir}" --silent`, { cwd: WEB });
  const reportPath = join(outDir, 'jscpd-report.json');
  if (!existsSync(reportPath)) {
    logFailure('web_duplicate_percent', r.stdout, r.stderr, 'jscpd não gerou relatório — duplicação pulada:');
    return null;
  }
  try {
    const data = JSON.parse(readFileSync(reportPath, 'utf8'));
    const pct = data?.statistics?.total?.percentage ?? 0;
    return parseFloat(Number(pct).toFixed(2));
  } catch (e) {
    warn('não foi possível parsear o relatório do jscpd: ' + e.message);
    return null;
  }
}

// npm audit --json em web → nº de vulnerabilidades "critical".
function collectWebAuditCritical() {
  if (!existsSync(WEB)) { warn('web/ não encontrado — audit pulado'); return null; }
  const r = run('npm audit --json', { cwd: WEB });
  const raw = r.stdout.trim();
  if (!raw) {
    logFailure('web_audit_critical', r.stdout, r.stderr, 'npm audit não produziu saída — métrica pulada:');
    return null;
  }
  try {
    const data = JSON.parse(raw);
    const vulns = data?.metadata?.vulnerabilities ?? data?.vulnerabilities ?? {};
    if (typeof vulns === 'object' && !Array.isArray(vulns)) {
      return vulns.critical ?? 0;
    }
    // Formato antigo (advisories).
    let crit = 0;
    for (const adv of Object.values(data?.advisories ?? {})) {
      if (adv.severity === 'critical') crit++;
    }
    return crit;
  } catch (e) {
    warn('não foi possível parsear o npm audit: ' + e.message);
    return null;
  }
}

// Type-check web (tsc -b): compila o projeto (com project references) e conta os
// erros de tipo. `--force` ignora o cache incremental para SEMPRE re-checar (o
// gate não pode passar só porque o .tsbuildinfo diz "up to date"). exit 0 = zero
// erros; caso contrário, conta as ocorrências de "error TSxxxx" na saída. Guarda
// contra regressão do shape gerado (ex.: fixture do guard-test desatualizado),
// que o vitest (esbuild, sem type-check) e o eslint não pegam.
function collectWebTypecheck() {
  if (!existsSync(WEB)) { warn('web/ não encontrado — type-check pulado'); return null; }
  const r = run('npx --yes tsc -b --force', { cwd: WEB });
  if (r.code === 0) return 0;
  const saida = `${r.stdout}\n${r.stderr}`;
  const m = saida.match(/error TS\d+/g);
  if (!m) {
    // saiu !=0 mas sem "error TS" reconhecível (ex.: tsc ausente/falha de setup):
    // pula em vez de bloquear indevidamente.
    logFailure('web_typecheck_errors', r.stdout, r.stderr, 'tsc -b falhou sem erros TS reconhecíveis — métrica "web_typecheck_errors" pulada:');
    return null;
  }
  return m.length;
}

// ESLint (flat config em web/): roda `eslint . -f json`, soma erros + avisos de
// todos os arquivos. O eslint sai 1 quando acha problema — isso é esperado; só
// falha real se o JSON não puder ser lido/parseado.
function collectWebLint() {
  if (!existsSync(WEB)) { warn('web/ não encontrado — lint pulado'); return null; }
  const r = run('npx --yes eslint . -f json', { cwd: WEB });
  const raw = r.stdout.trim();
  if (!raw) {
    logFailure('web_lint_issues', r.stdout, r.stderr, 'eslint não produziu saída JSON — métrica "web_lint_issues" pulada:');
    return null;
  }
  try {
    const files = JSON.parse(raw);
    if (!Array.isArray(files)) {
      warn('saída do eslint não é um array — métrica pulada');
      return null;
    }
    let total = 0;
    for (const f of files) {
      total += (f.errorCount ?? 0) + (f.warningCount ?? 0);
    }
    return total;
  } catch (e) {
    warn('não foi possível parsear o JSON do eslint: ' + e.message);
    return null;
  }
}

// Cobertura web (vitest + v8): roda `npm run test:coverage` em web/, lê
// coverage/coverage-summary.json → total.lines.pct. Se o arquivo não for gerado
// (ou os testes falharem em produzir o summary), a métrica é pulada.
function collectWebCoverage() {
  if (!existsSync(WEB)) { warn('web/ não encontrado — cobertura pulada'); return null; }
  const r = run('npm run test:coverage', { cwd: WEB });
  const summary = join(WEB, 'coverage', 'coverage-summary.json');
  if (!existsSync(summary)) {
    logFailure('web_coverage_lines', r.stdout, r.stderr, 'coverage-summary.json não gerado — métrica "web_coverage_lines" pulada:');
    return null;
  }
  try {
    const data = JSON.parse(readFileSync(summary, 'utf8'));
    const pct = data?.total?.lines?.pct;
    return typeof pct === 'number' ? pct : null;
  } catch (e) {
    warn('não foi possível parsear o coverage-summary.json: ' + e.message);
    return null;
  }
}

// ─── Coletores E2E (Camada 2, briefing 0068) ─────────────────────────────────
// Diferente de todos os coletores acima: NUNCA rodam o Playwright/build eles
// mesmos — só leem os dois artefatos que `make e2e-cov` já deixou prontos em
// e2e/.artifacts/. Se o artefato não existir (Playwright ainda não rodou),
// mesmo padrão dos demais coletores: avisa e devolve null (métrica pulada).
const E2E_ARTIFACTS = join(ROOT, 'e2e', '.artifacts');

// Lê o perfil textfmt que `coberturaBinario()` (e2e/global-teardown.ts) grava e
// extrai o total agregado via `go tool cover -func`, igual aos coletores Go.
function collectE2EBinaryCoverage() {
  const perfil = join(E2E_ARTIFACTS, 'e2e-binary-coverage.txt');
  if (!existsSync(perfil)) {
    warn('e2e/.artifacts/e2e-binary-coverage.txt não encontrado — rode `make e2e-cov` antes; métrica "e2e_binary_coverage_lines" pulada');
    return null;
  }
  const f = run(`go tool cover -func="${perfil}"`);
  if (f.code !== 0) {
    logFailure('e2e_binary_coverage_lines', f.stdout, f.stderr, 'go tool cover falhou sobre o perfil E2E — métrica pulada:');
    return null;
  }
  const line = f.stdout.trim().split('\n').filter((l) => l.startsWith('total:')).pop();
  const m = line && line.match(/(\d+(?:\.\d+)?)%/);
  return m ? parseFloat(m[1]) : null;
}

// Lê o JSON `{ linesPct }` que `coberturaFront()` (e2e/global-teardown.ts) grava
// a partir do relatório monocart.
function collectE2EFrontCoverage() {
  const arquivo = join(E2E_ARTIFACTS, 'e2e-front-coverage.json');
  if (!existsSync(arquivo)) {
    warn('e2e/.artifacts/e2e-front-coverage.json não encontrado — rode `make e2e-cov` antes; métrica "e2e_front_coverage_lines" pulada');
    return null;
  }
  try {
    const data = JSON.parse(readFileSync(arquivo, 'utf8'));
    return typeof data?.linesPct === 'number' ? data.linesPct : null;
  } catch (e) {
    warn('não foi possível parsear e2e-front-coverage.json: ' + e.message);
    return null;
  }
}

// Coletor de cada métrica, indexado pela mesma chave usada em METRIC_DEFS —
// permite iterar METRIC_DEFS e decidir o que rodar sem uma lista paralela.
const COLLECTORS = {
  go_coverage_lines:     collectGoCoverage,
  e2e_coverage_lines:    collectE2ECoverage,
  go_gofmt_unformatted:  collectGofmt,
  go_lint_issues:        collectGolangciLint,
  go_vuln_high:          collectGovulncheck,
  go_cyclo_violations:   collectGocyclo,
  web_coverage_lines:    collectWebCoverage,
  web_typecheck_errors:  collectWebTypecheck,
  web_duplicate_percent: collectWebDuplication,
  web_lint_issues:       collectWebLint,
  web_audit_critical:    collectWebAuditCritical,
  e2e_binary_coverage_lines: collectE2EBinaryCoverage,
  e2e_front_coverage_lines:  collectE2EFrontCoverage,
};

// Modo `--fast` (briefing 0061 §4.3): só os coletores baratos de cada lado.
const FAST_METRIC_KEYS = new Set([
  'go_coverage_lines',
  'go_gofmt_unformatted',
  'web_coverage_lines',
  'web_typecheck_errors',
]);

const SIDE_LABELS = { go: 'Go (raiz)', web: 'Web (web/)', e2e: 'E2E (navegador)' };
const SIDE_SKIP_REASONS = {
  go:  'Go pulado: sem mudanças em cmd/, internal/, go.mod ou go.sum neste diff.',
  web: 'Web pulado: sem mudanças em web/ neste diff.',
};

// ─── Coleta unificada ────────────────────────────────────────────────────────
// `touches` = { touchesGo, touchesWeb } (briefing 0061 §4.1). Quando um lado
// não foi tocado, PULA a chamada dos coletores desse lado inteiramente (não
// roda o comando e deixa a métrica null) — reaproveita o tratamento de null já
// existente em compareMetrics/printLocal/printMarkdown.
function collectMetrics(touches = { touchesGo: true, touchesWeb: true }, opts = {}) {
  const { fast = false } = opts;
  mkdirSync(REPORTS, { recursive: true });
  const metrics = {};
  const touchedBySide = { go: touches.touchesGo, web: touches.touchesWeb, e2e: true };

  // Modo --e2e (briefing 0068): só lê os artefatos do lado 'e2e' (não roda
  // Go/Web); fora desse modo, comportamento idêntico ao anterior (['go','web']).
  const sides = isE2E ? ['e2e'] : ['go', 'web'];

  for (const side of sides) {
    if (!touchedBySide[side]) {
      log(`${c.dim}${SIDE_SKIP_REASONS[side]}${c.reset}`);
      for (const [key, def] of Object.entries(METRIC_DEFS)) {
        if (def.side === side) metrics[key] = null;
      }
      continue;
    }
    log(`${c.dim}Coletando métricas ${SIDE_LABELS[side]}...${c.reset}`);
    for (const [key, def] of Object.entries(METRIC_DEFS)) {
      if (def.side !== side) continue;
      if (fast && !FAST_METRIC_KEYS.has(key)) {
        metrics[key] = null;
        continue;
      }
      metrics[key] = COLLECTORS[key]();
    }
  }

  return metrics;
}

// ─── Geração do baseline ─────────────────────────────────────────────────────
function generateBaseline() {
  log(`${c.bold}Gerando baseline de qualidade para ${PROJECT}...${c.reset}\n`);
  const novasMetricas = collectMetrics();

  // Sempre mescla por cima do baseline.json existente (se houver) — nunca
  // sobrescreve do zero. Necessário porque `--generate-baseline --e2e` só
  // recolhe o lado 'e2e'; sem a mesclagem, zeraria as chaves go_*/web_* vigentes.
  let anteriores = {};
  if (existsSync(BASELINE_PATH)) {
    try {
      const existing = JSON.parse(readFileSync(BASELINE_PATH, 'utf8'));
      anteriores = existing?.metrics ?? {};
    } catch (e) {
      warn('não foi possível ler o baseline.json existente (será tratado como vazio): ' + e.message);
    }
  }
  const metrics = { ...anteriores, ...novasMetricas };

  const baseline = {
    generatedAt: new Date().toISOString(),
    commit: (() => {
      const r = run('git rev-parse --short HEAD');
      return r.code === 0 ? r.stdout.trim() : 'unknown';
    })(),
    project: PROJECT,
    metrics,
  };

  writeFileSync(BASELINE_PATH, JSON.stringify(baseline, null, 2) + '\n', 'utf8');
  log(`\n${c.green}✔ Baseline gravado em baseline.json${c.reset}`);
  log(`${c.dim}Commite este arquivo para a catraca avançar ao longo do tempo.${c.reset}\n`);
  log('Métricas atuais:');
  for (const [key, val] of Object.entries(metrics)) {
    const def = METRIC_DEFS[key];
    const shown = val === null ? `${c.yellow}pulada${c.reset}` : `${c.cyan}${val}${c.reset} ${def?.unit ?? ''}`;
    log(`  ${def?.label ?? key}: ${shown}`);
  }
  log('');
}

// ─── Comparação (ratchet) ────────────────────────────────────────────────────
function compareMetrics(current, baseline) {
  const regressions = [];
  const improvements = [];
  const unchanged = [];

  for (const [key, def] of Object.entries(METRIC_DEFS)) {
    const cur = current[key] ?? null;
    const base = baseline[key] ?? null;

    if (cur === null) { unchanged.push({ key, def, cur: 'N/A', base }); continue; }
    if (base === null) { unchanged.push({ key, def, cur, base: 'N/A' }); continue; }

    const curNum  = typeof cur  === 'number' ? cur  : parseFloat(cur);
    const baseNum = typeof base === 'number' ? base : parseFloat(base);
    if (isNaN(curNum) || isNaN(baseNum)) { unchanged.push({ key, def, cur, base }); continue; }

    // blocking 'zero' — atual precisa ser 0, independente do baseline.
    if (def.blocking === 'zero') {
      if (curNum > 0) regressions.push({ key, def, cur: curNum, base: baseNum });
      else unchanged.push({ key, def, cur: curNum, base: baseNum });
      continue;
    }

    const threshold = 0.1; // tolera diferença de ponto flutuante
    const isRegression = def.lowerIsBetter
      ? curNum > baseNum + threshold
      : curNum < baseNum - threshold;
    const isImprovement = def.lowerIsBetter
      ? curNum < baseNum - threshold
      : curNum > baseNum + threshold;

    if (isRegression) regressions.push({ key, def, cur: curNum, base: baseNum });
    else if (isImprovement) improvements.push({ key, def, cur: curNum, base: baseNum });
    else unchanged.push({ key, def, cur: curNum, base: baseNum });
  }

  return { regressions, improvements, unchanged };
}

// ─── Formatação ──────────────────────────────────────────────────────────────
function fmtDelta(cur, base) {
  if (typeof cur !== 'number' || typeof base !== 'number') return '';
  const delta = cur - base;
  const sign = delta >= 0 ? '+' : '';
  const arrow = delta > 0 ? '↑' : (delta < 0 ? '↓' : '=');
  return `${sign}${delta.toFixed(2)} ${arrow}`;
}

// ─── Saída local (terminal) ──────────────────────────────────────────────────
function printLocal(current, result) {
  const { regressions, improvements, unchanged } = result;
  log(`${c.bold}Quality Gate — ${PROJECT}${c.reset}\n`);

  // Tabela atual × baseline × delta.
  log(`${c.bold}Métricas (atual × baseline × delta):${c.reset}`);
  const all = [...improvements, ...unchanged, ...regressions]
    .sort((a, b) => Object.keys(METRIC_DEFS).indexOf(a.key) - Object.keys(METRIC_DEFS).indexOf(b.key));
  for (const { key, def, cur, base } of all) {
    const delta = fmtDelta(cur, base);
    let tag = `${c.dim}─${c.reset}`;
    if (improvements.some((i) => i.key === key)) tag = `${c.green}✔${c.reset}`;
    else if (regressions.some((r) => r.key === key)) tag = def.blocking !== false ? `${c.red}✘${c.reset}` : `${c.yellow}⚠${c.reset}`;
    const curStr = cur === 'N/A' ? `${c.dim}pulada${c.reset}` : `${cur} ${def.unit}`;
    const baseStr = base === 'N/A' ? 'N/A' : `${base} ${def.unit}`;
    log(`  ${tag} ${def.label.padEnd(28)} ${String(curStr).padStart(6)}   base ${baseStr}   ${delta}`);
  }
  log('');

  const blocking = regressions.filter((r) => r.def.blocking !== false);
  const warnings = regressions.filter((r) => r.def.blocking === false);

  if (warnings.length > 0) {
    log(`${c.yellow}Avisos (não bloqueiam):${c.reset}`);
    for (const { def, cur, base } of warnings) {
      log(`  ${c.yellow}⚠${c.reset} ${def.label}: ${cur} ${def.unit} (era ${base} ${def.unit}  ${fmtDelta(cur, base)})`);
    }
    log('');
  }

  if (blocking.length > 0) {
    log(`${c.red}${c.bold}Regressões (BLOQUEANTES):${c.reset}`);
    for (const { def, cur, base } of blocking) {
      const causa = def.blocking === 'zero' ? `deve ser 0 (achou ${cur})` : `piorou vs baseline ${base}`;
      log(`  ${c.red}✘${c.reset} ${def.label}: ${c.red}${cur} ${def.unit}${c.reset} — ${causa}`);
    }
    log('');
    log(`${c.red}${c.bold}Quality gate FALHOU.${c.reset} Corrija as regressões acima e rode de novo:`);
    log(`  ${c.cyan}make gate-local${c.reset}\n`);
    return false;
  }

  log(`${c.green}${c.bold}Quality gate PASSOU.${c.reset}\n`);
  return true;
}

// ─── Saída CI (Markdown) ─────────────────────────────────────────────────────
function printMarkdown(current, result) {
  const { regressions, improvements, unchanged } = result;
  const blocking = regressions.filter((r) => r.def.blocking !== false);
  const warnings = regressions.filter((r) => r.def.blocking === false);
  const passed = blocking.length === 0;
  const status = passed ? '✅ Quality Gate PASSOU' : '❌ Quality Gate FALHOU';

  const lines = [];
  lines.push(`## ${status}`);
  lines.push('');
  lines.push(`**Projeto:** \`${PROJECT}\``);
  lines.push(`**Gerado:** ${new Date().toISOString()}`);
  lines.push('');
  lines.push('### Métricas');
  lines.push('');
  lines.push('| Lado | Métrica | Atual | Baseline | Delta | Status |');
  lines.push('|------|---------|-------|----------|-------|--------|');

  const allRows = [...improvements, ...unchanged, ...regressions]
    .sort((a, b) => Object.keys(METRIC_DEFS).indexOf(a.key) - Object.keys(METRIC_DEFS).indexOf(b.key));
  for (const { key, def, cur, base } of allRows) {
    const delta = (typeof cur === 'number' && typeof base === 'number') ? fmtDelta(cur, base) : '—';
    let icon = '➡️';
    if (improvements.some((i) => i.key === key)) icon = '✅';
    else if (regressions.some((r) => r.key === key)) icon = def.blocking !== false ? '❌' : '⚠️';
    const curStr = cur === 'N/A' ? '_pulada_' : `${cur} ${def.unit}`;
    const baseStr = base === 'N/A' ? '—' : `${base} ${def.unit}`;
    lines.push(`| ${def.side} | ${def.label} | ${curStr} | ${baseStr} | ${delta} | ${icon} |`);
  }
  lines.push('');

  if (blocking.length > 0) {
    lines.push('### ❌ Regressões bloqueantes');
    lines.push('');
    for (const { def, cur, base } of blocking) {
      const causa = def.blocking === 'zero' ? `deve ser 0 (achou ${cur} ${def.unit})` : `${cur} ${def.unit} ← era ${base} ${def.unit}`;
      lines.push(`- **${def.label}**: ${causa}`);
    }
    lines.push('');
    lines.push('> Corrija antes de mesclar. Se os novos valores forem intencionais e melhores, rode `make baseline` e commite o `baseline.json`.');
    lines.push('');
  }

  if (warnings.length > 0) {
    lines.push('### ⚠️ Avisos (não bloqueiam)');
    lines.push('');
    for (const { def, cur, base } of warnings) {
      lines.push(`- **${def.label}**: ${cur} ${def.unit} (era ${base} ${def.unit})`);
    }
    lines.push('');
  }

  if (improvements.length > 0) {
    lines.push('### ✅ Melhorias detectadas');
    lines.push('');
    for (const { def, cur, base } of improvements) {
      lines.push(`- **${def.label}**: ${cur} ${def.unit} (era ${base} ${def.unit})`);
    }
    lines.push('');
    lines.push('> Rode `make baseline` para avançar a catraca com essas melhorias.');
    lines.push('');
  }

  lines.push('---');
  lines.push('*Catraca unificada Go + TS — MyMediaDB (base: boatman)*');

  const markdown = lines.join('\n');
  log(markdown);
  mkdirSync(REPORTS, { recursive: true });
  const reportFile = isE2E ? 'e2e-quality-gate-report.md' : 'quality-gate-report.md';
  writeFileSync(join(REPORTS, reportFile), markdown + '\n', 'utf8');
  return passed;
}

// ─── Main ────────────────────────────────────────────────────────────────────
function main() {
  if (isGenBaseline) { generateBaseline(); return; }

  if (!existsSync(BASELINE_PATH)) {
    warn('baseline.json não encontrado. Rode: make baseline');
    warn('Pulando comparação da catraca.');
    process.exit(0);
  }

  let baseline;
  try {
    const raw = JSON.parse(readFileSync(BASELINE_PATH, 'utf8'));
    baseline = raw.metrics ?? raw;
  } catch (e) {
    warn('não foi possível ler baseline.json: ' + e.message);
    process.exit(1);
  }

  let touches;
  if (isFull) {
    touches = { touchesGo: true, touchesWeb: true };
    log(`${c.dim}--full: filtro por diff ignorado, rodando os dois lados completos.${c.reset}`);
  } else {
    const changed = getChangedFiles();
    touches = classifyDiff(changed);
    if (changed === null) {
      log(`${c.dim}Base do diff indeterminada — rodando os dois lados completos (fallback seguro).${c.reset}`);
    }
  }
  if (isFast) {
    warn('Modo --fast: só coletores baratos (go_coverage_lines, go_gofmt_unformatted, web_coverage_lines, web_typecheck_errors). Não substitui a rodada completa antes do push.');
  }

  const current = collectMetrics(touches, { fast: isFast });
  const result  = compareMetrics(current, baseline);

  const passed = isLocal ? printLocal(current, result) : printMarkdown(current, result);
  if (!passed) process.exit(1);
}

main();
