import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { globalIgnores } from 'eslint/config'

// Flat config (ESLint 10) equivalente ao template moderno do Vite React + TS:
// @eslint/js recommended + typescript-eslint recommended + react-hooks + a
// regra react-refresh do Vite. Aplica só ao nosso código (`**/*.{ts,tsx}`).
export default tseslint.config([
  // Artefatos gerados / não versionados como fonte — nunca linta. api-types.ts é
  // GERADO do openapi.yaml (npm run gen:types), como o sqlcgen no back: versionado
  // mas fora do lint (segue o padrão de components/ui).
  globalIgnores([
    'dist',
    'coverage',
    'node_modules',
    'node_modules_rootbak',
    'src/lib/api-types.ts',
  ]),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2023,
      globals: globals.browser,
    },
  },
  // shadcn/ui é código GERADO (não é nosso): os componentes exportam também
  // variantes/constantes ao lado do componente, o que dispara o aviso
  // `react-refresh/only-export-components`. Desligamos a regra SÓ aqui — sem
  // relaxar nada no nosso código.
  {
    files: ['src/components/ui/**'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
])
