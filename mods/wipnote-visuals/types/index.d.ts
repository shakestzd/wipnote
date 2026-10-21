declare module 'claude-code' {
  interface PluginState {
    'wipnote-visuals': {
      /** `/visuals on|off`: off removes the model rule and draws fences as plain text. */
      enabled: boolean
    }
  }
}
