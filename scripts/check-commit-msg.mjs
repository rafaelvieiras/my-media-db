#!/usr/bin/env node
/**
 * check-commit-msg.mjs — Valida Conventional Commits (leve, sem dependências).
 *
 * O MyMediaDB escreve commits em pt-BR no formato `tipo: resumo`
 * (ver CONTRIBUTING.md). Este checador é chamado pelo hook `commit-msg` do
 * lefthook com o caminho do arquivo da mensagem como argumento.
 *
 * Uso:
 *   node scripts/check-commit-msg.mjs <caminho-do-arquivo-de-mensagem>
 *   echo "feat: algo" | node scripts/check-commit-msg.mjs   # (stdin, p/ testes)
 *
 * Sai 0 se válido; !=0 com explicação se inválido.
 */

import { readFileSync } from 'node:fs';

const TYPES = ['feat', 'fix', 'docs', 'chore', 'refactor', 'test', 'style', 'perf', 'build', 'ci', 'revert'];

// tipo(escopo-opcional)!?: resumo    — escopo e "!" (breaking) opcionais.
const PATTERN = new RegExp(`^(${TYPES.join('|')})(\\([\\w.\\-/ ]+\\))?(!)?: .+`);

function readMessage() {
  const file = process.argv[2];
  if (file) {
    try { return readFileSync(file, 'utf8'); }
    catch (e) { console.error(`check-commit-msg: não foi possível ler ${file}: ${e.message}`); process.exit(2); }
  }
  // fallback: stdin (útil para testes)
  try { return readFileSync(0, 'utf8'); }
  catch { return ''; }
}

const raw = readMessage();
// Primeira linha não-comentário e não-vazia é o assunto do commit.
const subject = raw
  .split('\n')
  .filter((l) => !l.startsWith('#'))
  .map((l) => l.trimEnd())
  .find((l) => l.trim() !== '') ?? '';

// Ignora commits especiais que o git gera (merge/revert/fixup automáticos).
if (/^(Merge|Revert|fixup!|squash!)/.test(subject)) {
  process.exit(0);
}

if (!PATTERN.test(subject)) {
  console.error('');
  console.error('✘ Mensagem de commit fora do padrão Conventional Commits.');
  console.error('');
  console.error(`  Recebido: "${subject}"`);
  console.error('');
  console.error('  Formato esperado:  tipo: resumo   (em pt-BR, imperativo)');
  console.error(`  Tipos válidos:     ${TYPES.join(', ')}`);
  console.error('  Exemplos:          feat: adiciona busca incremental');
  console.error('                     fix(server): trata 404 de API em JSON');
  console.error('');
  process.exit(1);
}

if (subject.length > 100) {
  console.error(`✘ Assunto do commit muito longo (${subject.length} > 100 caracteres). Encurte o resumo.`);
  process.exit(1);
}

process.exit(0);
