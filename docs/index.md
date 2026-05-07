---
hide:
  - navigation
  - toc
title: wipnote
---

<div class="hg-hero" markdown>

<div class="hg-hero__bolt">
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
    <path d="M13 2 3 14h9l-1 8 10-12h-9l1-8z"/>
  </svg>
</div>

<h1 class="hg-hero__headline">wipnote</h1>

<p class="hg-hero__sub">
Local-first observability and coordination platform for AI-assisted development.
</p>

<p class="hg-hero__solution">
Work items, session tracking, custom agents, hooks, slash commands, quality gates,
and a real-time dashboard &mdash; managed by a single Go binary, stored as HTML files in your repo.
No external infrastructure required.
</p>

<div class="hg-hero__buttons">
  <a class="hg-btn hg-btn--primary" href="#install">Install</a>
  <a class="hg-btn hg-btn--secondary" href="reference/cli/">CLI Reference</a>
  <a class="hg-btn hg-btn--secondary" href="blog/">Blog</a>
</div>

</div>

<!-- ======================================== -->

<section class="hg-section" markdown>

<h2 class="hg-section__title">What it does</h2>

<div class="hg-cards hg-cards--3col" markdown>

<div class="hg-card" markdown>
<span class="hg-card__title">Work item tracking</span>

Features, bugs, spikes, and tracks as HTML files in `.wipnote/`. Every change is a git diff. Every item has a lifecycle: create, start, complete.
</div>

<div class="hg-card" markdown>
<span class="hg-card__title">Session observability</span>

Hooks capture every tool call, every prompt, and attribute them to the active work item. See exactly what happened in any session via the dashboard.
</div>

<div class="hg-card" markdown>
<span class="hg-card__title">Custom agents</span>

Define specialized agents with specific models, tools, and system prompts. A researcher agent for investigation, a coder for implementation, a test runner for quality &mdash; each scoped to its job.
</div>

<div class="hg-card" markdown>
<span class="hg-card__title">Hooks &amp; automation</span>

Event-driven hooks on SessionStart, PreToolUse, PostToolUse, and Stop. Enforce safety rules, capture telemetry, block dangerous operations, or trigger custom workflows automatically.
</div>

<div class="hg-card" markdown>
<span class="hg-card__title">Skills &amp; slash commands</span>

Reusable workflows as slash commands: `/deploy`, `/diagnose`, `/plan`, `/code-quality`. Package complex multi-step procedures into single invocations that agents and humans can both use.
</div>

<div class="hg-card" markdown>
<span class="hg-card__title">Quality gates</span>

Enforce software engineering discipline: build, lint, and test before every commit. Spec compliance scoring, code health metrics, and structured diff reviews built into the CLI.
</div>

<div class="hg-card" markdown>
<span class="hg-card__title">Real-time dashboard</span>

Activity feed, kanban board, session viewer, and work item detail &mdash; served locally by `wipnote serve`. See what every agent is doing right now.
</div>

<div class="hg-card" markdown>
<span class="hg-card__title">Multi-agent coordination</span>

Claude Code, Gemini CLI, Codex, and GitHub Copilot all read from and write to the same work items. Orchestration patterns control which agent handles which task.
</div>

<div class="hg-card" markdown>
<span class="hg-card__title">Plans &amp; specifications</span>

CRISPI plans break initiatives into trackable steps. Feature specs define acceptance criteria. Agents execute against the plan and report progress.
</div>

</div>

</section>

<!-- ======================================== -->

<section class="hg-section" markdown>

<h2 class="hg-section__title">Everything is a file in your repo</h2>

<div class="hg-cards hg-cards--3col hg-cards--arch" markdown>

<div class="hg-card hg-card--arch" markdown>
<code class="hg-card__label">.wipnote/*.html</code>

**HTML files** &mdash; Work items are the source of truth. Human-readable. Git-diffable. No proprietary format.
</div>

<div class="hg-card hg-card--arch" markdown>
<code class="hg-card__label">.wipnote/wipnote.db</code>

**SQLite index** &mdash; A derived read index for fast queries and dashboard rendering. Gitignored. Rebuilt from HTML anytime.
</div>

<div class="hg-card hg-card--arch" markdown>
<code class="hg-card__label">wipnote</code>

**Go binary** &mdash; One CLI that does everything: create work items, manage sessions, serve the dashboard, run hooks.
</div>

</div>

</section>

<!-- ======================================== -->

<section class="hg-section" markdown>

<h2 class="hg-section__title">Quick start</h2>

```bash
# Initialize in your repo
wipnote init

# Create a track and feature
wipnote track create "Auth Overhaul"
wipnote feature create "Add OAuth support" --track trk-abc123 --description "Implement OAuth2 flow"
wipnote feature start feat-def456

# Work with any AI agent — context is shared
# ... Claude Code, Gemini, Codex all see the active work item ...

wipnote feature complete feat-def456
wipnote serve    # see everything at localhost:4000
```

</section>

<!-- ======================================== -->

<section class="hg-section" id="install" markdown>

<h2 class="hg-section__title">Install</h2>

```bash
# Install (universal)
curl -fsSL https://raw.githubusercontent.com/shakestzd/wipnote/main/install.sh | sh

# Or as a Claude Code plugin
claude plugin install wipnote

# Or build from source
git clone https://github.com/shakestzd/wipnote.git
cd wipnote && go build -o wipnote ./cmd/wipnote/
```

### Upgrading

```bash
wipnote upgrade            # latest release
wipnote upgrade --check    # check without installing
wipnote update             # alias for upgrade
```

</section>

<!-- ======================================== -->

<section class="hg-section" markdown>

<h2 class="hg-section__title">Work item types</h2>

| Type | Prefix | Purpose |
|------|--------|---------|
| Feature | `feat-` | Units of deliverable work |
| Bug | `bug-` | Defects to fix |
| Spike | `spk-` | Time-boxed investigations |
| Track | `trk-` | Initiatives grouping related work |
| Plan | `plan-` | CRISPI implementation plans |

</section>

<!-- ======================================== -->

<div class="hg-footer-links" markdown>

[CLI Reference](reference/cli.md) &nbsp;&middot;&nbsp; [Blog](blog/index.md) &nbsp;&middot;&nbsp; [GitHub](https://github.com/shakestzd/wipnote) &nbsp;&middot;&nbsp; [Claude Code Plugin](https://github.com/shakestzd/wipnote)

</div>
