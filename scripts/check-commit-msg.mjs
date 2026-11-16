#!/usr/bin/env node
/**
 * check-commit-msg.mjs — Validates Conventional Commits (lightweight, no dependencies).
 *
 * MyMediaDB writes commits in pt-BR in the `tipo: resumo` format
 * (see CONTRIBUTING.md). This checker is called by the lefthook `commit-msg` hook,
 * with the path to the message file as the argument.
 *
 * Usage:
 *   node scripts/check-commit-msg.mjs <path-to-message-file>
 *   echo "feat: algo" | node scripts/check-commit-msg.mjs   # (stdin, for tests)
 *
 * Exits 0 when valid; !=0 with an explanation when invalid.
 */

import { readFileSync } from 'node:fs';

const TYPES = ['feat', 'fix', 'docs', 'chore', 'refactor', 'test', 'style', 'perf', 'build', 'ci', 'revert'];

// tipo(optional-scope)!?: resumo    — scope and "!" (breaking) are optional.
const PATTERN = new RegExp(`^(${TYPES.join('|')})(\\([\\w.\\-/ ]+\\))?(!)?: .+`);

function readMessage() {
  const file = process.argv[2];
  if (file) {
    try { return readFileSync(file, 'utf8'); }
    catch (e) { console.error(`check-commit-msg: não foi possível ler ${file}: ${e.message}`); process.exit(2); }
  }
  // fallback: stdin (useful for tests)
  try { return readFileSync(0, 'utf8'); }
  catch { return ''; }
}

const raw = readMessage();
// The first non-comment and non-empty line is the commit subject.
const subject = raw
  .split('\n')
  .filter((l) => !l.startsWith('#'))
  .map((l) => l.trimEnd())
  .find((l) => l.trim() !== '') ?? '';

// Ignores the special commits that git generates (automatic merge/revert/fixup).
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
