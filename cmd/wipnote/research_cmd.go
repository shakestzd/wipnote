package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/research"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

// Research-first gate CLI (docs/design/research-gate.md). The rule itself is
// research.Rule; this file only wires it to commands.

// applyChangeKind records the --change-kind flag, or infers one from the title
// and prints a note saying so. Spikes get no kind.
func applyChangeKind(col *workitem.Collection, typeName string, node *models.Node, flag string) {
	kind, matched := flag, ""
	if kind == "" {
		kind, matched = research.InferKind(typeName, node.Title)
		if kind == "" {
			return
		}
		why := "the " + typeName + " default"
		if matched != "" {
			why = fmt.Sprintf("title keyword %q", matched)
		}
		fmt.Fprintf(os.Stderr, "note: change kind %q inferred from %s; override with --change-kind or `wipnote %s research kind %s <kind>`\n",
			kind, why, typeName, node.ID)
	}
	if err := col.Edit(node.ID).SetProperty(research.PropKind, kind).Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save change kind: %v\n", err)
		return
	}
	if node.Properties == nil {
		node.Properties = map[string]any{}
	}
	node.Properties[research.PropKind] = kind
}

// printResearchRecord renders a node's change kind and research record; used by
// `<kind> show` and `lineage`. Prints nothing for an item with neither.
func printResearchRecord(w io.Writer, n *models.Node, indent string) {
	kind := research.KindOf(n.Properties)
	rec := research.FromNode(n)
	if kind == "" && rec == nil {
		return
	}
	fmt.Fprintf(w, "\n%sResearch:\n", indent)
	in := indent + "  "
	if kind != "" {
		fmt.Fprintf(w, "%schange kind  %s\n", in, kind)
	}
	if rec == nil {
		fmt.Fprintf(w, "%s(no research record)\n", in)
		return
	}
	line := func(label, v string) {
		if v != "" {
			fmt.Fprintf(w, "%s%-12s %s\n", in, label, v)
		}
	}
	line("question", rec.Question)
	for _, s := range rec.Sources {
		fmt.Fprintf(w, "%s%-12s %s [%s %s]\n", in, "source", s.URL, orDash(s.Type), orDash(s.Retrieved))
	}
	line("findings", rec.Findings)
	line("exists", rec.Exists)
	for _, q := range rec.Queries {
		line("query", q)
	}
	for _, o := range rec.Options {
		line("option", o)
	}
	line("chosen", rec.Chosen)
	for _, r := range rec.Rejected {
		line("rejected", r.Option+" — "+r.Reason)
	}
	for _, g := range rec.Gaps {
		line("gap", g)
	}
	line("waiver", rec.Waiver)
	line("from spike", rec.SpikeID)
}

// saveResearchRecord merges add into the item's record and persists it.
func saveResearchRecord(col *workitem.Collection, id string, add *research.Record) (*research.Record, error) {
	node, err := col.Get(id)
	if err != nil {
		return nil, err
	}
	merged := research.Merge(research.FromNode(node), add)
	if err := col.Edit(id).SetProperty(research.PropRecord, merged.Property()).Save(); err != nil {
		return nil, fmt.Errorf("save research record on %s: %w", id, err)
	}
	return merged, nil
}

// openItem resolves id and returns the project and the collection holding it.
// Only features, bugs and spikes carry research records.
func openItem(id string) (string, *workitem.Project, *workitem.Collection, string, error) {
	dir, err := findWipnoteDir()
	if err != nil {
		return "", nil, nil, "", err
	}
	if id, err = resolveID(dir, id); err != nil {
		return "", nil, nil, "", err
	}
	typeName := kindFromPrefix(id)
	if !researchTypes[typeName] {
		return "", nil, nil, "", fmt.Errorf("%s is a %s; research records live on features, bugs and spikes", id, typeName)
	}
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return "", nil, nil, "", fmt.Errorf("open project: %w", err)
	}
	return dir, p, collectionFor(p, typeName), id, nil
}

// researchTypes are the work-item types that carry a research record.
var researchTypes = map[string]bool{"feature": true, "bug": true, "spike": true}

type researchAddOpts struct {
	question, findings, exists, chosen string
	primary, secondary, queries        []string
	options, rejected, gaps            []string
	retrieved                          string
}

func (o *researchAddOpts) record() (*research.Record, error) {
	rec := &research.Record{
		Question: o.question, Findings: o.findings, Exists: o.exists, Chosen: o.chosen,
		Queries: o.queries, Options: o.options, Gaps: o.gaps,
	}
	date := o.retrieved
	if date == "" {
		date = nowDate()
	}
	for _, group := range []struct {
		typ  string
		urls []string
	}{{research.SourcePrimary, o.primary}, {research.SourceSecondary, o.secondary}} {
		typ := group.typ
		for _, u := range group.urls {
			if !wiIsResearchURL(u) {
				return nil, fmt.Errorf("source %q must be an http(s) URL with a host", u)
			}
			rec.Sources = append(rec.Sources, research.Source{URL: u, Type: typ, Retrieved: date})
		}
	}
	for _, r := range o.rejected {
		// Split on ": " (colon then space) so an option holding a URL or
		// host:port keeps its own colons.
		opt, reason, ok := strings.Cut(r, ": ")
		if !ok || strings.TrimSpace(opt) == "" || strings.TrimSpace(reason) == "" {
			return nil, fmt.Errorf("--rejected %q must be \"<option>: <reason>\"", r)
		}
		rec.Rejected = append(rec.Rejected, research.Rejected{Option: strings.TrimSpace(opt), Reason: strings.TrimSpace(reason)})
	}
	return rec, nil
}

// wiResearchCmd is `wipnote <kind> research` with add, show, kind and waive.
func wiResearchCmd(typeName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "research",
		Short: "Record or show the research record on a " + typeName,
		Long:  research.Rule,
	}
	cmd.AddCommand(wiResearchAddCmd(typeName), wiResearchShowCmd(), wiResearchKindCmd(), wiResearchWaiveCmd())
	return cmd
}

func wiResearchAddCmd(typeName string) *cobra.Command {
	var o researchAddOpts
	cmd := &cobra.Command{
		Use:   "add <id>",
		Short: "Add to the research record of a " + typeName + " (lists append, text fields overwrite)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			rec, err := o.record()
			if err != nil {
				return err
			}
			_, p, col, id, err := openItem(args[0])
			if err != nil {
				return err
			}
			defer p.Close()
			merged, err := saveResearchRecord(col, id, rec)
			if err != nil {
				return err
			}
			fmt.Printf("Research recorded on %s\n", id)
			if missing, warns := research.Assess(merged); len(missing)+len(warns) > 0 {
				for _, w := range warns {
					fmt.Fprintf(os.Stderr, "research warning: %s\n", w)
				}
				if len(missing) > 0 {
					fmt.Fprintf(os.Stderr, "still missing for the gate: %s\n", strings.Join(missing, "; "))
				}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.question, "question", "", "the question the research answers")
	f.StringArrayVar(&o.primary, "primary", nil, "primary source URL (official docs, spec, changelog); repeatable")
	f.StringArrayVar(&o.secondary, "secondary", nil, "secondary source URL (blog, forum, article); repeatable")
	f.StringVar(&o.retrieved, "retrieved", "", "retrieval date for the sources in this call (YYYY-MM-DD, default today)")
	f.StringVar(&o.findings, "findings", "", "what the sources established")
	f.StringVar(&o.exists, "exists", "", "what the project, its dependencies or known patterns already provide (from `wipnote relevant` and code search), or \"none found\"")
	f.StringArrayVar(&o.queries, "query", nil, "a search run to fill --exists; required with \"none found\"; repeatable")
	f.StringArrayVar(&o.options, "option", nil, "an option considered; repeatable")
	f.StringVar(&o.chosen, "chosen", "", "the chosen option (the recorded decision)")
	f.StringArrayVar(&o.rejected, "rejected", nil, "\"<option>: <reason>\" for a rejected option; repeatable")
	f.StringArrayVar(&o.gaps, "gap", nil, "an evidence gap: a source that failed to load or a claim not verified; repeatable")
	return cmd
}

func wiResearchShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show the research record and what the gate still needs",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			dir, p, col, id, err := openItem(args[0])
			if err != nil {
				return err
			}
			defer p.Close()
			node, err := col.Get(id)
			if err != nil {
				return err
			}
			printResearchRecord(os.Stdout, node, "")
			cfg := research.LoadConfig(filepath.Dir(dir))
			warns, gateErr := research.CheckComplete(cfg, node)
			for _, w := range warns {
				fmt.Println(w)
			}
			if gateErr != nil {
				fmt.Println(gateErr.Error())
			}
			return nil
		},
	}
}

func wiResearchKindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kind <id> <" + strings.Join(research.AllKinds, "|") + ">",
		Short: "Set the change kind that decides whether the research gate applies",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if !research.ValidKind(args[1]) {
				return fmt.Errorf("change kind %q is not one of %s", args[1], strings.Join(research.AllKinds, ", "))
			}
			_, p, col, id, err := openItem(args[0])
			if err != nil {
				return err
			}
			defer p.Close()
			if err := col.Edit(id).SetProperty(research.PropKind, args[1]).Save(); err != nil {
				return err
			}
			fmt.Printf("Change kind of %s set to %s\n", id, args[1])
			return nil
		},
	}
}

func wiResearchWaiveCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "waive <id> [reason]",
		Short: "Waive the research gate for an item (the reason is recorded), or --clear a waiver",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			if clear == (len(args) == 2) {
				return fmt.Errorf("give either a reason or --clear")
			}
			_, p, col, id, err := openItem(args[0])
			if err != nil {
				return err
			}
			defer p.Close()
			if clear {
				if err := clearResearchWaiver(col, id); err != nil {
					return err
				}
				fmt.Printf("Research waiver cleared on %s\n", id)
				return nil
			}
			if err := recordResearchWaiver(col, id, args[1]); err != nil {
				return err
			}
			fmt.Printf("Research waived on %s: %s\n", id, args[1])
			return nil
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "remove a waiver recorded earlier")
	return cmd
}

// clearResearchWaiver removes the waiver from an item's record (Merge never
// clears a field, so this is the one way back from a mistaken waiver).
func clearResearchWaiver(col *workitem.Collection, id string) error {
	node, err := col.Get(id)
	if err != nil {
		return err
	}
	rec := research.FromNode(node)
	if rec == nil || rec.Waiver == "" {
		return nil
	}
	rec.Waiver = ""
	return col.Edit(id).SetProperty(research.PropRecord, rec.Property()).Save()
}

func nowDate() string { return time.Now().Format("2006-01-02") }

// recordResearchWaiver stores a waiver rationale on the item's research record.
func recordResearchWaiver(col *workitem.Collection, id, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("research waiver needs a reason")
	}
	_, err := saveResearchRecord(col, id, &research.Record{Waiver: reason})
	return err
}

// researchCmd is the top-level `wipnote research` command.
func researchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "research",
		Short: "Research-first gate: start a research spike for a work item",
		Long: research.Rule + "\n\nConfig (.wipnote/config.json): {\"research\": {\"mode\": \"off|warn|block\", " +
			"\"required_kinds\": [...]}}; default mode warn, kinds " + strings.Join(research.DefaultRequiredKinds, ", ") + ".",
	}
	cmd.AddCommand(researchStartCmd())
	return cmd
}

func researchStartCmd() *cobra.Command {
	var forID string
	cmd := &cobra.Command{
		Use:   "start <question> --for <id>",
		Short: "Create a research spike for a work item; completing it attaches the record to the item",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if forID == "" {
				return fmt.Errorf("--for <work-item-id> is required")
			}
			spike, err := startResearchSpike(args[0], forID)
			if err != nil {
				return err
			}
			fmt.Printf("Created: %s  %s\n", spike.ID, spike.Title)
			fmt.Printf("Fill it with: wipnote spike research add %s --primary <url> --exists ... --option ... --chosen ...\n", spike.ID)
			fmt.Printf("Then: wipnote spike complete %s  (attaches the record to %s)\n", spike.ID, spike.Properties[research.PropFor])
			return nil
		},
	}
	cmd.Flags().StringVar(&forID, "for", "", "work item (feature, bug or spike) the research is for")
	return cmd
}

// researchSpikeTemplate is the spike body: the fields to fill, in order.
const researchSpikeTemplate = `Research spike for %s. Record with ` + "`wipnote spike research add %s`" + `:
question; sources (--primary/--secondary, retrieved date); findings;
exists (` + "`wipnote relevant <topic>`" + ` + code search, or "none found" with --query);
options; chosen; rejected ("<option>: <reason>"); gaps (failed sources, unverified claims).
Completing this spike attaches the record to %s.`

// startResearchSpike creates a spike linked to the item it researches.
func startResearchSpike(question, forID string) (*models.Node, error) {
	_, p, col, parentID, err := openItem(forID)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	parent, err := col.Get(parentID)
	if err != nil {
		return nil, err
	}
	spike, err := p.Spikes.Create("Research: "+question,
		workitem.SpikeWithTrack(parent.TrackID),
		workitem.SpikeWithType("technical"))
	if err != nil {
		return nil, fmt.Errorf("create research spike: %w", err)
	}
	rec := &research.Record{Question: question}
	if err := p.Spikes.Edit(spike.ID).
		SetDescription(fmt.Sprintf(researchSpikeTemplate, parentID, spike.ID, parentID)).
		SetProperty(research.PropFor, parentID).
		SetProperty(research.PropRecord, rec.Property()).Save(); err != nil {
		return nil, fmt.Errorf("link research spike: %w", err)
	}
	if _, err := p.Spikes.AddEdge(spike.ID, models.Edge{
		TargetID: parentID, Relationship: models.RelSpawnedFrom, Title: parent.Title, Since: time.Now().UTC(),
	}); err != nil {
		return nil, fmt.Errorf("link research spike: %w", err)
	}
	return p.Spikes.Get(spike.ID)
}

// attachSpikeResearch copies a completed research spike's record onto the item
// it was started for. Non-fatal: a failure is reported, completion stands.
func attachSpikeResearch(w io.Writer, p *workitem.Project, spikeID string) {
	spike, err := p.Spikes.Get(spikeID)
	if err != nil {
		return
	}
	parentID, _ := spike.Properties[research.PropFor].(string)
	rec := research.FromNode(spike)
	if parentID == "" || rec == nil {
		return
	}
	rec.SpikeID = spikeID
	if _, err := saveResearchRecord(collectionFor(p, kindFromPrefix(parentID)), parentID, rec); err != nil {
		fmt.Fprintf(w, "research: could not attach %s findings to %s: %v\n", spikeID, parentID, err)
		return
	}
	fmt.Fprintf(w, "research: attached %s findings to %s as its research record\n", spikeID, parentID)
	// The completion commit only covers the spike's own artifact; persist the
	// parent's under the same artifact commit policy (defer queues an intent,
	// none leaves it to the operator, separate commits directly).
	if parentType := kindFromPrefix(parentID); shouldAutocommitWorkitemArtifact(parentType) {
		if err := persistWorkitemArtifactTransition(p.ProjectDir, parentType, parentID, "research attached"); err != nil {
			fmt.Fprintf(w, "research: %v\n", err)
		}
	}
}

// printLineageResearch prints the research record of a lineage root work item.
func printLineageResearch(w io.Writer, wipnoteDir, id string) {
	path := resolveNodePath(wipnoteDir, id)
	if path == "" {
		return
	}
	if n, err := htmlparse.ParseFile(path); err == nil {
		printResearchRecord(w, n, "  ")
	}
}
