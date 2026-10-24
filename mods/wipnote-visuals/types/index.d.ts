declare module 'claude-code' {
  interface PluginState {
    'wipnote-visuals': {
      /** `/visuals on|off`: off removes the model rule and draws fences as plain text. */
      enabled: boolean
      /** `/visuals receipt on|off`: off stops the receipt and score line under each turn. */
      receipt: boolean
      /** The main loop's tool calls so far this turn; reset at turn.complete. */
      turn: {
        files: string[]
        commands: { label: string; kind: 'test' | 'build' | 'commit' | 'other'; outcome: 'ok' | 'failed' | 'unknown'; exitCode?: number }[]
        commits: string[]
      }
      /** `/plainspeak strict|80|off`: which rules feed the note for the next prompt; off sends none. */
      plainspeak: 'strict' | '80' | 'off'
      /** The writing note the last reply earned, sent as context with the next prompt; '' when clean. */
      lintNote: string
    }
  }
}
