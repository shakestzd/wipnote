package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	dbpkg "github.com/shakestzd/htmlgraph/internal/db"
	"github.com/shakestzd/htmlgraph/internal/planamend"
	"github.com/shakestzd/htmlgraph/internal/planchat"
	"github.com/shakestzd/htmlgraph/internal/planyaml"
	"github.com/shakestzd/htmlgraph/internal/plantmpl"
)

// planListItem is a single entry in the GET /api/plans response.
type planListItem struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	FeatureID  string    `json:"feature_id"`
	Approved   int       `json:"approved"`
	Total      int       `json:"total"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// plansListHandler returns a JSON array of all plans sorted by mtime desc.
// GET /api/plans
func plansListHandler(htmlgraphDir string, database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		plansDir := filepath.Join(htmlgraphDir, "plans")
		entries, err := os.ReadDir(plansDir)
		if err != nil {
			if os.IsNotExist(err) {
				respondJSON(w, []planListItem{})
				return
			}
			http.Error(w, fmt.Sprintf("reading plans dir: %v", err), http.StatusInternalServerError)
			return
		}

		var items []planListItem
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") {
				continue
			}
			planID := strings.TrimSuffix(entry.Name(), ".html")
			planPath := filepath.Join(plansDir, entry.Name())

			item, err := parsePlanListItem(planPath, planID, database)
			if err != nil {
				continue
			}
			items = append(items, item)
		}

		sort.Slice(items, func(i, j int) bool {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		})

		if items == nil {
			items = []planListItem{}
		}
		respondJSON(w, items)
	}
}

// parsePlanListItem reads a plan HTML file and extracts list metadata.
// Merges approval counts from SQLite (live feedback) with HTML (static defaults).
func parsePlanListItem(planPath, planID string, database *sql.DB) (planListItem, error) {
	info, err := os.Stat(planPath)
	if err != nil {
		return planListItem{}, err
	}

	f, err := os.Open(planPath)
	if err != nil {
		return planListItem{}, err
	}
	defer f.Close()

	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		return planListItem{}, err
	}

	article := doc.Find("article[id]").First()
	featureID, _ := article.Attr("data-feature-id")

	// Read status from YAML source of truth; fall back to HTML attribute for
	// backward compatibility (missing YAML, broken envs, existing tests).
	status := ""
	yamlPath := strings.TrimSuffix(planPath, ".html") + ".yaml"
	if yamlPlan, yamlErr := planyaml.Load(yamlPath); yamlErr == nil {
		status = yamlPlan.Meta.Status
	}
	if status == "" {
		// Fallback: parse HTML attribute (legacy — YAML is the canonical source).
		status, _ = article.Attr("data-status")
	}
	if status == "" {
		status = "draft"
	}

	title := strings.TrimSpace(doc.Find("h1").First().Text())
	if title == "" {
		title = planID
	}

	// Count total approve checkboxes from HTML (defines the section count).
	var total int
	doc.Find("input[data-action='approve']").Each(func(_ int, s *goquery.Selection) {
		total++
	})

	// Get live approval count from SQLite (overrides HTML checked attrs).
	approved := 0
	if database != nil {
		feedback, err := dbpkg.GetPlanFeedback(database, planID)
		if err == nil {
			for _, fb := range feedback {
				if fb.Action == "approve" && fb.Value == "true" {
					approved++
				}
			}
		}
		// Also check if finalized in SQLite — either fully approved or explicit flag.
		if status != "finalized" {
			isApproved, _ := dbpkg.IsPlanFullyApproved(database, planID)
			if isApproved {
				status = "finalized"
			}
		}
		// Final fallback: check explicit finalize flag stored during API finalize call.
		if status != "finalized" {
			var flagVal string
			_ = database.QueryRow(
				"SELECT value FROM plan_feedback WHERE plan_id = ? AND section = 'meta' AND action = 'finalize'",
				planID,
			).Scan(&flagVal)
			if flagVal == "true" {
				status = "finalized"
			}
		}
	}

	// Fall back to HTML checked attrs if SQLite has nothing
	if approved == 0 {
		doc.Find("input[data-action='approve']").Each(func(_ int, s *goquery.Selection) {
			if _, exists := s.Attr("checked"); exists {
				approved++
			}
		})
	}

	return planListItem{
		ID:        planID,
		Title:     title,
		Status:    status,
		FeatureID: featureID,
		Approved:  approved,
		Total:     total,
		UpdatedAt: info.ModTime().UTC(),
	}, nil
}

// planStatusResponse is returned by GET /api/plans/{id}/status.
type planStatusResponse struct {
	PlanID        string `json:"plan_id"`
	Status        string `json:"status"`
	ApprovedCount int    `json:"approved_count"`
	TotalSections int    `json:"total_sections"`
}

// planFeedbackResponse is returned by GET /api/plans/{id}/feedback.
type planFeedbackResponse struct {
	PlanID       string                     `json:"plan_id"`
	Status       string                     `json:"status"`
	Sections     map[string]sectionFeedback `json:"sections"`
	Questions    map[string]string          `json:"questions"`
	ChatMessages []chatMessageEntry         `json:"chat_messages,omitempty"`
}

type sectionFeedback struct {
	Approved bool   `json:"approved"`
	Comment  string `json:"comment"`
}

// chatMessageEntry is a single chat message in the feedback response.
type chatMessageEntry struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

// planFeedbackRequest is the body for POST /api/plans/{id}/feedback.
type planFeedbackRequest struct {
	Section    string `json:"section"`
	Action     string `json:"action"`
	Value      string `json:"value"`
	QuestionID string `json:"question_id"`
}

// planFileHandler serves HTML plan files from .htmlgraph/plans/{id}.html.
// GET /plans/{id}.html
func planFileHandler(htmlgraphDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// URL path: /plans/{id}.html — strip the /plans/ prefix.
		name := strings.TrimPrefix(r.URL.Path, "/plans/")
		if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
			http.Error(w, "invalid plan path", http.StatusBadRequest)
			return
		}
		if !strings.HasSuffix(name, ".html") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		planPath := filepath.Join(htmlgraphDir, "plans", name)
		if _, err := os.Stat(planPath); err != nil {
			http.Error(w, "plan not found", http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, planPath)
	}
}

// planStatusHandler returns status information for a plan.
// GET /api/plans/{id}/status
func planStatusHandler(database *sql.DB, htmlgraphDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		planID, err := extractPlanID(r.URL.Path, "/status")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		planPath, err := resolvePlanPath(htmlgraphDir, planID)
		if err != nil {
			http.Error(w, "plan not found", http.StatusNotFound)
			return
		}

		htmlStatus, err := parsePlanHTMLStatus(planPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("reading plan: %v", err), http.StatusInternalServerError)
			return
		}

		approvedCount, totalSections, err := countPlanSections(database, planID)
		if err != nil {
			http.Error(w, fmt.Sprintf("querying feedback: %v", err), http.StatusInternalServerError)
			return
		}

		respondJSON(w, planStatusResponse{
			PlanID:        planID,
			Status:        htmlStatus,
			ApprovedCount: approvedCount,
			TotalSections: totalSections,
		})
	}
}

// validSectionRe matches valid plan feedback section keys used by:
//
//	design                          — design approvals
//	outline                         — outline approvals
//	meta                            — plan metadata actions (e.g. finalize flag)
//	critique                        — critique section approvals
//	chat                            — chat session messages
//	q-<name>                        — question answers (legacy)
//	slice-<num>                     — slice-level approval (slice-4)
//	slice-<num>-question-<id>       — slice-local question answer (slice-4)
var validSectionRe = regexp.MustCompile(`^(design|outline|meta|critique|chat|slice-\d+-question-[a-z0-9-]+|slice-\d+|q-[a-z0-9-]+)$`)

// planFeedbackSubmitHandler stores a feedback entry for a plan section.
// POST /api/plans/{id}/feedback
func planFeedbackSubmitHandler(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req planFeedbackRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
			return
		}
		if req.Section == "" || req.Action == "" {
			http.Error(w, "section and action are required", http.StatusBadRequest)
			return
		}

		// Normalize underscores to hyphens for slice sections — a common
		// mistake that used to silently store wrong-format keys that
		// finalize-yaml couldn't find.
		// Handles both 'slice_N' and 'slice_N_question_<id>' patterns (slice-4).
		if rest, ok := strings.CutPrefix(req.Section, "slice_"); ok {
			req.Section = "slice-" + strings.ReplaceAll(rest, "_", "-")
		}

		if !validSectionRe.MatchString(req.Section) {
			http.Error(w, fmt.Sprintf("invalid section %q — must match: design, outline, meta, critique, chat, slice-N, slice-N-question-<id>, or q-<name>", req.Section), http.StatusBadRequest)
			return
		}

		planID, err := extractPlanID(r.URL.Path, "/feedback")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := dbpkg.StorePlanFeedback(database, planID, req.Section, req.Action, req.Value, req.QuestionID); err != nil {
			http.Error(w, fmt.Sprintf("storing feedback: %v", err), http.StatusInternalServerError)
			return
		}

		respondJSON(w, map[string]string{"status": "ok"})
	}
}

// planFinalizeHandler finalizes a plan once all sections are approved.
// POST /api/plans/{id}/finalize
func planFinalizeHandler(database *sql.DB, htmlgraphDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		planID, err := extractPlanID(r.URL.Path, "/finalize")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		approved, err := dbpkg.IsPlanFullyApproved(database, planID)
		if err != nil {
			http.Error(w, fmt.Sprintf("checking approval: %v", err), http.StatusInternalServerError)
			return
		}
		if !approved {
			http.Error(w, "not all sections approved", http.StatusBadRequest)
			return
		}

		if err := dbpkg.FinalizePlan(database, planID); err != nil {
			http.Error(w, fmt.Sprintf("finalizing plan: %v", err), http.StatusInternalServerError)
			return
		}

		// Store explicit finalization flag so list queries detect finalized state
		// even when the HTML write fails (HTML is canonical but SQLite is fallback).
		if err := dbpkg.StorePlanFeedback(database, planID, "meta", "finalize", "true", ""); err != nil {
			log.Printf("warning: store finalize flag for %s: %v", planID, err)
		}

		// Write finalized HTML snapshot with all feedback baked in.
		planPath, err := resolvePlanPath(htmlgraphDir, planID)
		if err == nil {
			if err := finalizePlanHTML(planPath, database, planID); err != nil {
				log.Printf("warning: finalizePlanHTML failed for %s: %v", planID, err)
			}
		}

		// Keep YAML meta.status in sync with HTML finalization so YAML remains
		// the source of truth for status reads (parsePlanHTMLStatus, plan wait, etc.).
		if yamlErr := updatePlanStatus(htmlgraphDir, planID, "finalized"); yamlErr != nil {
			log.Printf("warning: updatePlanStatus(finalized) failed for %s: %v", planID, yamlErr)
		}

		feedback, err := dbpkg.GetPlanFeedback(database, planID)
		if err != nil {
			http.Error(w, fmt.Sprintf("reading feedback: %v", err), http.StatusInternalServerError)
			return
		}

		// Create track and features from approved slices, mirroring what the CLI
		// does via `htmlgraph plan finalize-yaml`. Partial failures are logged and
		// reported in the response — a finalized plan with N/M features created is
		// better than aborting and leaving the plan in a half-finalized state.
		createdFeatures, featFailures, featErr := finalizeYAMLWithDB(database, htmlgraphDir, planID)
		if featErr != nil {
			log.Printf("warning: finalizeYAMLWithDB failed for %s: %v", planID, featErr)
		}
		for _, f := range featFailures {
			log.Printf("warning: plan %s slice %d (%s): feature creation failed: %s", planID, f.SliceNum, f.Title, f.Error)
		}
		if createdFeatures == nil {
			createdFeatures = []string{}
		}

		type failureInfo struct {
			SliceNum int    `json:"slice_num"`
			Title    string `json:"title"`
			Error    string `json:"error"`
		}
		var failureInfos []failureInfo
		for _, f := range featFailures {
			failureInfos = append(failureInfos, failureInfo{SliceNum: f.SliceNum, Title: f.Title, Error: f.Error})
		}
		if failureInfos == nil {
			failureInfos = []failureInfo{}
		}

		respondJSON(w, map[string]any{
			"plan_id":          planID,
			"status":           "finalized",
			"feedback":         feedback,
			"created_features": createdFeatures,
			"failures":         failureInfos,
		})
	}
}

// planFeedbackReadHandler returns structured feedback for a plan.
// GET /api/plans/{id}/feedback
func planFeedbackReadHandler(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		planID, err := extractPlanID(r.URL.Path, "/feedback")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		entries, err := dbpkg.GetPlanFeedback(database, planID)
		if err != nil {
			http.Error(w, fmt.Sprintf("reading feedback: %v", err), http.StatusInternalServerError)
			return
		}

		respondJSON(w, buildFeedbackResponse(planID, entries))
	}
}

// planFeedbackHandler routes GET and POST for /api/plans/{id}/feedback.
func planFeedbackHandler(database *sql.DB) http.HandlerFunc {
	submitH := planFeedbackSubmitHandler(database)
	readH := planFeedbackReadHandler(database)
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			submitH(w, r)
		case http.MethodGet:
			readH(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// planDeleteHandler deletes a draft plan's HTML file and feedback.
// DELETE /api/plans/{id}/delete
func planDeleteHandler(database *sql.DB, htmlgraphDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		planID, err := extractPlanID(r.URL.Path, "/delete")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		planPath, err := resolvePlanPath(htmlgraphDir, planID)
		if err != nil {
			http.Error(w, "plan not found", http.StatusNotFound)
			return
		}

		htmlStatus, err := parsePlanHTMLStatus(planPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("reading plan: %v", err), http.StatusInternalServerError)
			return
		}

		// Only allow deletion of draft or in-progress plans
		if htmlStatus == "finalized" {
			http.Error(w, "Cannot delete a finalized plan", http.StatusBadRequest)
			return
		}

		// Delete the HTML file
		if err := os.Remove(planPath); err != nil {
			http.Error(w, fmt.Sprintf("deleting plan file: %v", err), http.StatusInternalServerError)
			return
		}

		// Delete feedback from SQLite
		if err := dbpkg.DeletePlanFeedback(database, planID); err != nil {
			http.Error(w, fmt.Sprintf("deleting feedback: %v", err), http.StatusInternalServerError)
			return
		}

		respondJSON(w, map[string]string{"status": "deleted", "plan_id": planID})
	}
}

// planChatRequest is the body for POST /api/plans/{id}/chat.
type planChatRequest struct {
	Message string `json:"message"`
}

// planChatHandler streams Claude responses as SSE for a plan chat session.
// POST /api/plans/{id}/chat
func planChatHandler(database *sql.DB, htmlgraphDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		planID, err := extractPlanID(r.URL.Path, "/chat")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var req planChatRequest
		body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil {
			http.Error(w, "reading request body", http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
			return
		}
		if req.Message == "" {
			http.Error(w, "message is required", http.StatusBadRequest)
			return
		}

		// Load plan YAML for context.
		planContext := loadPlanContext(htmlgraphDir, planID)

		// Resolve project dir (parent of .htmlgraph/).
		projectDir := filepath.Dir(htmlgraphDir)

		backend := planchat.New(database, planID, planContext, projectDir)
		if !backend.IsAvailable() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Claude unavailable. Install claude CLI or set ANTHROPIC_API_KEY.",
			})
			return
		}

		// Store user message.
		_ = backend.SaveMessage("user", req.Message)

		// Set SSE headers.
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Stream response.
		chunks, errCh := backend.Send(r.Context(), req.Message)

		var fullResponse strings.Builder
		for chunk := range chunks {
			fullResponse.WriteString(chunk)
			payload, _ := json.Marshal(map[string]string{
				"type": "chunk",
				"text": chunk,
			})
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}

		// Check for errors.
		if err := <-errCh; err != nil {
			payload, _ := json.Marshal(map[string]string{
				"type":  "error",
				"error": err.Error(),
			})
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}

		// Store assistant message.
		if fullResponse.Len() > 0 {
			_ = backend.SaveMessage("assistant", fullResponse.String())

			// Detect and store AMEND directives from the assistant response.
			amendments := planamend.ParseAmendments(fullResponse.String())
			for _, a := range amendments {
				section := fmt.Sprintf("slice-%d", a.SliceNum)
				value, _ := json.Marshal(a)
				if err := dbpkg.StorePlanFeedback(database, planID, section, "amendment", string(value), ""); err != nil {
					log.Printf("warning: store amendment for plan %s slice %d: %v", planID, a.SliceNum, err)
				}
			}
		}

		// Send done event.
		fmt.Fprintf(w, "data: %s\n\n", `{"type":"done"}`)
		flusher.Flush()
	}
}

// loadPlanContext reads the plan YAML file for use as Claude context.
// Falls back to empty string if the file is not found.
func loadPlanContext(htmlgraphDir, planID string) string {
	yamlPath := filepath.Join(htmlgraphDir, "plans", planID+".yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		// Try HTML fallback for plan context.
		htmlPath := filepath.Join(htmlgraphDir, "plans", planID+".html")
		data, err = os.ReadFile(htmlPath)
		if err != nil {
			return ""
		}
	}
	return string(data)
}

// planRouter dispatches /api/plans/{id}/{action} to the appropriate handler.
// Registered under /api/plans/ in serve.go.
func planRouter(database *sql.DB, htmlgraphDir string) http.HandlerFunc {
	statusH := planStatusHandler(database, htmlgraphDir)
	feedbackH := planFeedbackHandler(database)
	finalizeH := planFinalizeHandler(database, htmlgraphDir)
	deleteH := planDeleteHandler(database, htmlgraphDir)
	chatH := planChatHandler(database, htmlgraphDir)
	amendmentsH := planAmendmentsHandler(database)
	yamlH := planYAMLHandler(htmlgraphDir)
	renderH := planRenderHandler(database, htmlgraphDir)
	eventsH := planEventsHandler(database)
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/chat"):
			chatH(w, r)
		case strings.HasSuffix(path, "/render"):
			renderH(w, r)
		case strings.HasSuffix(path, "/events"):
			eventsH(w, r)
		case strings.HasSuffix(path, "/status"):
			statusH(w, r)
		case strings.HasSuffix(path, "/feedback"):
			feedbackH(w, r)
		case strings.HasSuffix(path, "/finalize"):
			finalizeH(w, r)
		case strings.HasSuffix(path, "/delete"):
			deleteH(w, r)
		case strings.HasSuffix(path, "/amendments"):
			amendmentsH(w, r)
		case strings.HasSuffix(path, "/yaml"):
			yamlH(w, r)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}
}

// planRenderHandler dynamically renders plan HTML from the YAML source.
// Returns just the plan content (no outer HTML shell/sidebar) for embedding
// in the dashboard detail panel.
// GET /api/plans/{id}/render
func planRenderHandler(database *sql.DB, htmlgraphDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		planID, err := extractPlanID(r.URL.Path, "/render")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Build a PlanPage dynamically from the YAML source so the content
		// is always up-to-date — the static HTML may be stale or empty.
		page := plantmpl.BuildFromTopic(planID, "", "", "")
		enrichPageFromYAML(htmlgraphDir, planID, page)
		enrichRelatedWork(database, page)

		// If YAML enrichment didn't populate the title, fall back to
		// extracting it from the static HTML file.
		if page.Title == "" {
			htmlPath := filepath.Join(htmlgraphDir, "plans", planID+".html")
			if data, err := os.ReadFile(htmlPath); err == nil {
				if doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data))); err == nil {
					page.Title = doc.Find("h1").First().Text()
				}
			}
		}
		if page.Title == "" {
			page.Title = planID
		}

		// Render the full page, then extract styles + article + scripts
		// so the embedded view has complete CSS and interactivity.
		var buf strings.Builder
		if err := page.Render(&buf); err != nil {
			http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(buf.String()))
		if err != nil {
			http.Error(w, "parse error", http.StatusInternalServerError)
			return
		}

		var out strings.Builder

		// Include plan CSS, stripping rules that would conflict with
		// the dashboard shell (body grid, html reset, sidebar styles).
		doc.Find("style").Each(func(_ int, s *goquery.Selection) {
			css, _ := s.Html()
			if css == "" {
				return
			}
			// Remove rules that target body/html layout (they'd override dashboard)
			for _, strip := range []string{
				":root{", ":root {", "[data-theme",
				"*,", "body{", "html{",
				".plan-sidebar{", ".plan-sidebar ",
				".plan-sidebar.", "@media(max-width",
			} {
				for {
					idx := strings.Index(css, strip)
					if idx < 0 {
						break
					}
					// Find the matching closing brace
					depth := 0
					end := idx
					for i := idx; i < len(css); i++ {
						if css[i] == '{' {
							depth++
						} else if css[i] == '}' {
							depth--
							if depth == 0 {
								end = i + 1
								break
							}
						}
					}
					css = css[:idx] + css[end:]
				}
			}
			out.WriteString("<style>")
			out.WriteString(css)
			out.WriteString("</style>\n")
		})

		// Include CDN link tags (highlight.js, fonts)
		doc.Find("link[rel='stylesheet'], link[rel='preconnect']").Each(func(_ int, s *goquery.Selection) {
			outerHTML, _ := goquery.OuterHtml(s)
			out.WriteString(outerHTML)
			out.WriteString("\n")
		})

		// Plan layout (content + chat sidebar + drag handle).
		// Extract .plan-layout which wraps article + chat-sidebar,
		// falling back to article or body if layout wrapper missing.
		layout, _ := goquery.OuterHtml(doc.Find(".plan-layout").First())
		if layout == "" {
			layout, _ = doc.Find("article").Html()
		}
		if layout == "" {
			layout, _ = doc.Find("body").Html()
		}
		out.WriteString(layout)

		// Include scripts (D3, dagre-d3, plan JS)
		doc.Find("script").Each(func(_ int, s *goquery.Selection) {
			outerHTML, _ := goquery.OuterHtml(s)
			out.WriteString(outerHTML)
			out.WriteString("\n")
		})

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, out.String())
	}
}

// enrichRelatedWork populates the plan page's related track and features
// by looking up their titles and statuses from the features table.
func enrichRelatedWork(database *sql.DB, page *plantmpl.PlanPage) {
	if database == nil {
		return
	}

	// Look up the linked track
	if page.FeatureID != "" {
		var title, status string
		err := database.QueryRow(
			`SELECT COALESCE(title, id), COALESCE(status, 'todo') FROM features WHERE id = ?`,
			page.FeatureID,
		).Scan(&title, &status)
		if err == nil && title != "" {
			page.RelatedTrack = &plantmpl.RelatedWorkItem{
				ID:     page.FeatureID,
				Title:  title,
				Type:   "track",
				Status: status,
			}
		}
	}

	// Look up slice features
	for _, sc := range page.Slices {
		if sc.ID == "" {
			continue
		}
		var title, status string
		err := database.QueryRow(
			`SELECT COALESCE(title, id), COALESCE(status, 'todo') FROM features WHERE id = ?`,
			sc.ID,
		).Scan(&title, &status)
		if err != nil {
			title = sc.Title
			status = "todo"
		}
		page.RelatedFeatures = append(page.RelatedFeatures, plantmpl.RelatedWorkItem{
			ID:     sc.ID,
			Title:  title,
			Type:   "feature",
			Status: status,
		})
	}
}

// planEventsHandler streams plan feedback changes as SSE.
// GET /api/plans/{id}/events
func planEventsHandler(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		planID, err := extractPlanID(r.URL.Path, "/events")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		var lastRowID int64
		database.QueryRow(
			`SELECT COALESCE(MAX(id), 0) FROM plan_feedback WHERE plan_id = ?`,
			planID,
		).Scan(&lastRowID)

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				database.Exec("PRAGMA wal_checkpoint(PASSIVE)")
				rows, err := database.Query(`
					SELECT id, section, action, value
					FROM plan_feedback
					WHERE plan_id = ? AND id > ?
					ORDER BY id ASC LIMIT 20`, planID, lastRowID)
				if err != nil {
					continue
				}
				for rows.Next() {
					var id int64
					var section, action, value string
					if err := rows.Scan(&id, &section, &action, &value); err != nil {
						continue
					}
					payload, _ := json.Marshal(map[string]string{
						"plan_id": planID, "section": section,
						"action": action, "value": value,
					})
					fmt.Fprintf(w, "data: %s\n\n", payload)
					lastRowID = id
				}
				rows.Close()
				flusher.Flush()
			}
		}
	}
}

// planYAMLHandler serves the raw YAML source for a plan.
// GET /api/plans/{id}/yaml
func planYAMLHandler(htmlgraphDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		planID, err := extractPlanID(r.URL.Path, "/yaml")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data, err := os.ReadFile(filepath.Join(htmlgraphDir, "plans", planID+".yaml"))
		if err != nil {
			http.Error(w, "plan YAML not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(data)
	}
}

// planAmendmentsHandler returns amendments parsed from plan feedback entries.
// GET /api/plans/{id}/amendments
func planAmendmentsHandler(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		planID, err := extractPlanID(r.URL.Path, "/amendments")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		entries, err := dbpkg.GetPlanFeedback(database, planID)
		if err != nil {
			http.Error(w, fmt.Sprintf("reading feedback: %v", err), http.StatusInternalServerError)
			return
		}

		type amendmentEntry struct {
			planamend.Amendment
			Status string `json:"status"` // pending, accepted, rejected
		}

		// Build a map from question_id -> status for amendment_status entries.
		statusMap := make(map[string]string)
		for _, e := range entries {
			if e.Action == "amendment_status" && e.QuestionID != "" {
				statusMap[e.QuestionID] = e.Value
			}
		}

		var amendments []amendmentEntry
		for _, e := range entries {
			if e.Action != "amendment" {
				continue
			}
			var a planamend.Amendment
			if err := json.Unmarshal([]byte(e.Value), &a); err != nil {
				continue
			}
			status := "pending"
			if s, ok := statusMap[e.QuestionID]; ok {
				status = s
			}
			amendments = append(amendments, amendmentEntry{Amendment: a, Status: status})
		}

		if amendments == nil {
			amendments = []amendmentEntry{}
		}
		respondJSON(w, amendments)
	}
}

// ---- helpers ----------------------------------------------------------------

// extractPlanID parses a plan ID from URL paths of the form
// /api/plans/{id}/{suffix}. Returns an error if the ID is missing.
func extractPlanID(urlPath, suffix string) (string, error) {
	const prefix = "/api/plans/"
	path := strings.TrimSuffix(urlPath, "/")
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("unexpected path: %s", urlPath)
	}
	mid := path[len(prefix):]
	mid = strings.TrimSuffix(mid, suffix)
	if mid == "" || strings.Contains(mid, "/") {
		return "", fmt.Errorf("missing or invalid plan ID in path: %s", urlPath)
	}
	return mid, nil
}

// resolvePlanPath returns the absolute path to a plan's HTML file, or an
// error if the file does not exist.
func resolvePlanPath(htmlgraphDir, planID string) (string, error) {
	p := filepath.Join(htmlgraphDir, "plans", planID+".html")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("plan %s not found", planID)
	}
	return p, nil
}

// parsePlanHTMLStatus reads the plan's YAML source of truth and returns
// meta.status. The planPath argument is the HTML path; YAML is derived via
// TrimSuffix so callers do not need to change their invocations.
func parsePlanHTMLStatus(planPath string) (string, error) {
	yamlPath := strings.TrimSuffix(planPath, ".html") + ".yaml"
	plan, err := planyaml.Load(yamlPath)
	if err != nil {
		return "", fmt.Errorf("load plan YAML for status: %w", err)
	}
	status := plan.Meta.Status
	if status == "" {
		status = "draft"
	}
	return status, nil
}

// finalizePlanHTML writes a snapshot of the finalized plan with all feedback
// baked into the HTML: checkboxes checked, radio buttons selected, comments
// filled, and data-status set to "finalized". The HTML file becomes a
// self-contained record of the finalized plan.
func finalizePlanHTML(planPath string, database *sql.DB, planID string) error {
	data, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
	if err != nil {
		return err
	}

	// Set article status to finalized
	doc.Find("article").First().SetAttr("data-status", "finalized")

	// Read all feedback from SQLite
	feedback, err := dbpkg.GetPlanFeedback(database, planID)
	if err != nil {
		return err
	}

	for _, fb := range feedback {
		switch fb.Action {
		case "approve":
			// Handle approval inputs. For slice-card YAML plans, these are radio buttons
			// with three values (approved, changes_requested, rejected). For legacy plans,
			// these are checkboxes. Must branch on type to preserve radio-group invariant.
			section := fb.Section
			approved := fb.Value == "true"

			// For radios: set checked only on value='approved' if approved, clear all otherwise
			doc.Find(fmt.Sprintf("input[type='radio'][data-section='%s'][data-action='approve']", section)).
				Each(func(_ int, s *goquery.Selection) {
					val, _ := s.Attr("value")
					if approved && val == "approved" {
						s.SetAttr("checked", "checked")
					} else {
						s.RemoveAttr("checked")
					}
				})

			// For checkboxes: set checked only if approved
			if approved {
				doc.Find(fmt.Sprintf("input[type='checkbox'][data-section='%s'][data-action='approve']", section)).
					SetAttr("checked", "checked")
			} else {
				doc.Find(fmt.Sprintf("input[type='checkbox'][data-section='%s'][data-action='approve']", section)).
					RemoveAttr("checked")
			}
		case "comment":
			// Set textarea content for this section
			doc.Find(fmt.Sprintf("textarea[data-section='%s']", fb.Section)).
				SetText(fb.Value)
		case "answer":
			// Select the radio button matching this answer
			doc.Find(fmt.Sprintf("input[type='radio'][data-question='%s']", fb.QuestionID)).
				Each(func(_ int, s *goquery.Selection) {
					val, _ := s.Attr("value")
					if val == fb.Value {
						s.SetAttr("checked", "checked")
					} else {
						s.RemoveAttr("checked")
					}
				})
		}
	}

	html, err := doc.Html()
	if err != nil {
		return err
	}
	return os.WriteFile(planPath, []byte(html), 0o644)
}

// countPlanSections returns the count of approved sections and the total
// distinct sections with any feedback for the given plan.
func countPlanSections(database *sql.DB, planID string) (approved, total int, err error) {
	err = database.QueryRow(`
		SELECT
			COUNT(DISTINCT CASE WHEN action = 'approve' AND value = 'true' THEN section END),
			COUNT(DISTINCT section)
		FROM plan_feedback
		WHERE plan_id = ?`, planID,
	).Scan(&approved, &total)
	return
}

// buildFeedbackResponse groups raw feedback entries into the structured
// response consumed by the CLI and other API callers.
func buildFeedbackResponse(planID string, entries []dbpkg.PlanFeedback) planFeedbackResponse {
	sections := make(map[string]sectionFeedback)
	questions := make(map[string]string)
	approvedSections := make(map[string]bool)
	var chatMessages []chatMessageEntry

	for _, e := range entries {
		switch e.Action {
		case "approve":
			sf := sections[e.Section]
			sf.Approved = e.Value == "true"
			sections[e.Section] = sf
			if sf.Approved {
				approvedSections[e.Section] = true
			} else {
				delete(approvedSections, e.Section)
			}
		case "comment":
			sf := sections[e.Section]
			sf.Comment = e.Value
			sections[e.Section] = sf
		case "answer":
			if e.QuestionID != "" {
				questions[e.QuestionID] = e.Value
			}
		case "messages":
			// Chat messages stored as a JSON array under section='chat'.
			if e.Section == "chat" && e.Value != "" {
				var msgs []chatMessageEntry
				if json.Unmarshal([]byte(e.Value), &msgs) == nil {
					chatMessages = msgs
				}
			}
		}
	}

	// Exclude chat section from approval status calculation.
	delete(sections, "chat")
	delete(approvedSections, "chat")

	status := "draft"
	if len(sections) > 0 && len(approvedSections) == len(sections) {
		status = "approved"
	}

	return planFeedbackResponse{
		PlanID:       planID,
		Status:       status,
		Sections:     sections,
		Questions:    questions,
		ChatMessages: chatMessages,
	}
}
