/**
 * Registers @hanzo/gui's v5 config with the type system so the shorthand style
 * props the @hanzo/ui components take (`items` / `justify` / `bg` / `rounded` …)
 * resolve to their concrete types.
 *
 * The v5 config sets `onlyShorthandStyleProps`, so the shorthands ARE the API —
 * without this augmentation TypeScript sees only the base React Native props and
 * reads every one of them as unknown. Ambient + type-only.
 */
import type { createGui } from '@hanzo/gui'
import type { defaultConfig } from '@hanzogui/config/v5'

type Conf = ReturnType<typeof createGui<typeof defaultConfig>>

declare module '@hanzogui/web' {
  interface GuiCustomConfig extends Conf {}
}

declare module '@hanzogui/core' {
  interface GuiCustomConfig extends Conf {}
}
