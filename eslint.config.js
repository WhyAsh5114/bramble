import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import prettier from 'eslint-config-prettier'
import globals from 'globals'

export default tseslint.config(
  {
    ignores: [
      '**/node_modules/**',
      '**/dist/**',
      '**/bin/**',
      'sidecar/test/fixtures/**',
      'scripts/gate0.3-eac-check/**',
      'docs/**',
      // Next applications are linted by their package tasks with eslint-config-next.
      // Root ESLint 10 cannot load that package's ESLint 9 React rules.
      'dashboard/**',
      'approval/**',
      '.tmp/**',
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    languageOptions: {
      globals: { ...globals.node },
    },
    rules: {
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
    },
  },
  prettier
)
