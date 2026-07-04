/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Hanzo IAM base URL (HIP-0111 serverUrl). Default: https://hanzo.id */
  readonly VITE_IAM_URL?: string;
  /** OAuth2 client_id for the Base admin app (`<org>-base`). Default: hanzo-base */
  readonly VITE_IAM_CLIENT_ID?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
