import type { CodegenConfig } from '@graphql-codegen/cli'

// Typed documents for the native API (/api/graphql); run `pnpm codegen` after schema changes.
const config: CodegenConfig = {
  schema: '../api/graphql/native/*.graphqls',
  documents: ['src/**/*.{ts,tsx}', '!src/gql/**'],
  ignoreNoDocuments: false,
  generates: {
    'src/gql/': {
      preset: 'client',
      presetConfig: { fragmentMasking: false },
      config: {
        useTypeImports: true,
        enumsAsTypes: true,
        scalars: { Time: 'string' },
      },
    },
  },
}

export default config
