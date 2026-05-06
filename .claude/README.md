# Claude Code Configuration

## Project Setup

This project uses both **project-level hooks** and the **wipnote plugin**.

### Hooks Configuration

**For Development (this repo)**:
- Project hooks: `.claude/hooks/hooks.json` (uses `.claude/hooks/scripts/*`)
- Plugin hooks: Disabled via `.claude/settings.local.json`
- Why: Prevents duplicate hook execution during development

**For Web Usage**:
- Project hooks work without plugin installation
- Enables wipnote tracking in Claude Code web interface

**For Plugin Users (other projects)**:
- Plugin provides: hooks, skills, commands, templates
- Installed via: `claude plugin install wipnote@wipnote`
- Hooks use: `${CLAUDE_PLUGIN_ROOT}/hooks/scripts/*`

### Settings Files

- `.claude/settings.json` - Team settings (committed to git)
- `.claude/settings.local.json` - Personal overrides (gitignored)
- `.claude/hooks/hooks.json` - Project-level hooks (committed to git)

### Why settings.local.json?

When developing wipnote, you need:
1. ✅ Plugin installed (for skills, commands, templates)
2. ✅ Project hooks active (for development and web usage)
3. ❌ Plugin hooks disabled (to prevent duplicates)

The `.claude/settings.local.json` file achieves this by overriding plugin hooks with an empty object.

## For Contributors

If you're contributing to wipnote:

1. The plugin will be installed automatically (from marketplace)
2. Create your own `.claude/settings.local.json` to disable plugin hooks:
   ```json
   {
     "hooks": {}
   }
   ```
3. This file is gitignored - it's your personal configuration
4. Project hooks will run instead (needed for web usage)

## For Plugin Users

If you're using wipnote in another project:

1. Install the plugin: `claude plugin install wipnote@wipnote`
2. Plugin hooks will work automatically
3. You don't need project-level hooks
