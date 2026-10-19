package tools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"pentagi/pkg/database/knowledge/limits"
	"pentagi/pkg/graph/model"
	"pentagi/pkg/growth"
)

// FileOp is a type alias (not a distinct type) for String: it shares the
// exact same lenient UnmarshalJSON, so every FileOp value - including the
// ReadFile/WriteFile/EditFile constants below - automatically benefits from
// the quote-unwrapping behaviour documented on String, with zero changes
// needed at any existing call site (switches, comparisons, assignments)
// elsewhere in the codebase.
type FileOp = String

const (
	ReadFile  FileOp = "read_file"
	WriteFile FileOp = "write_file"
	EditFile  FileOp = "edit_file"
)

// FileAction's field descriptions are layered on purpose, each one adding
// only what the level above doesn't already say: the tool description (see
// registry.go) gives the read/write/edit decision; Action's enum values name
// which payload field each one consumes; Content/Diff each spec out the
// format of their own payload. Path/Message never vary by action, so they're
// described once, action-agnostically.
type FileAction struct {
	Action  FileOp `json:"action" jsonschema:"required,type=string,enum=read_file,enum=write_file,enum=edit_file" jsonschema_description:"'read_file' reads the file (no other field needed). 'write_file' overwrites it with 'content' (the whole file). 'edit_file' applies the patch in 'diff' (existing content elsewhere is untouched)."`
	Content string `json:"content,omitempty" jsonschema_description:"write_file only: the complete new file content (not a diff, not a partial update)."`
	Diff    String `json:"diff,omitempty" jsonschema:"type=string" jsonschema_description:"edit_file only: unified-diff hunk(s) - '@@ -old +new @@' header, then ' '/'-'/'+' lines. Always keep at least one unchanged context line so the location is unambiguous; context/removed lines must match the file's current content verbatim (read_file first). Header line numbers are only a hint - the line text is what must match."`
	Path    String `json:"path" jsonschema:"required,type=string" jsonschema_description:"Absolute path to the file, taken literally. This field is not run through a shell, so command substitutions, variables and globs would become part of the filename - resolve them with the terminal tool first and pass the result."`
	Message string `json:"message" jsonschema:"required,title=File action message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary describing what you are reading, writing, or editing and why. Written in the engagement language declared by your system prompt."`
}

// BrowserAction is a type alias for String - see the FileOp comment above.
type BrowserAction = String

const (
	Markdown BrowserAction = "markdown"
	HTML     BrowserAction = "html"
	Links    BrowserAction = "links"
)

type Browser struct {
	Url     string        `json:"url" jsonschema:"required" jsonschema_description:"URL to open in the browser"`
	Action  BrowserAction `json:"action" jsonschema:"required,type=string,enum=markdown,enum=html,enum=links" jsonschema_description:"Action to perform in the browser. 'markdown' - Returns the content of the page in markdown format. 'html' - Returns the content of the page in html format. 'links' - Get the list of all URLs on the page to be used in later calls (e.g., open search results after the initial search lookup)."`
	Message string        `json:"message" jsonschema:"required,title=Browser action message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary describing what content you are fetching, in which format, and why. Written in the engagement language declared by your system prompt."`
}

type SubtaskInfo struct {
	Title       string `json:"title" jsonschema:"required,title=Subtask title" jsonschema_description:"Engagement-log plan entry — short title of this subtask capturing its main goal as it appears in the engagement plan. Written in the engagement language declared by your system prompt."`
	Description string `json:"description" jsonschema:"required,title=Subtask to complete" jsonschema_description:"Engagement-log plan entry — full description of this subtask (instructions, rules, requirements, success criteria) as it appears in the engagement plan. Written in the engagement language declared by your system prompt."`
}

type SubtaskList struct {
	Subtasks SubtaskInfos `json:"subtasks" jsonschema:"required,title=Subtasks to complete" jsonschema_description:"Ordered list of subtasks produced by decomposing the task. Each subtask's title and description are engagement-log plan entries (see SubtaskInfo) — written in the engagement language declared by your system prompt."`
	Message  string       `json:"message" jsonschema:"required,title=Subtask generation result" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on the generation result and the main goal of the plan. Written in the engagement language declared by your system prompt."`
}

// SubtaskOperationType defines the type of operation to perform on a subtask.
// It is a type alias for String - see the FileOp comment above.
type SubtaskOperationType = String

const (
	SubtaskOpAdd     SubtaskOperationType = "add"
	SubtaskOpRemove  SubtaskOperationType = "remove"
	SubtaskOpModify  SubtaskOperationType = "modify"
	SubtaskOpReorder SubtaskOperationType = "reorder"
)

// SubtaskOperation defines a single operation on the subtask list for delta-based refinement
type SubtaskOperation struct {
	Op          SubtaskOperationType `json:"op" jsonschema:"required,type=string,enum=add,enum=remove,enum=modify,enum=reorder" jsonschema_description:"Operation type: 'add' creates a new subtask, 'remove' deletes a subtask by ID, 'modify' updates title/description of existing subtask, 'reorder' moves a subtask to a different position"`
	ID          *int64               `json:"id,omitempty" jsonschema:"title=Subtask ID" jsonschema_description:"ID of existing subtask (required for remove/modify/reorder operations)"`
	AfterID     *int64               `json:"after_id,omitempty" jsonschema:"title=Insert after ID" jsonschema_description:"For add/reorder: insert after this subtask ID (null/0 = insert at beginning)"`
	Title       string               `json:"title,omitempty" jsonschema:"title=New title" jsonschema_description:"Engagement-log plan entry — new subtask title (required for add, optional for modify). Written in the engagement language declared by your system prompt."`
	Description string               `json:"description,omitempty" jsonschema:"title=New description" jsonschema_description:"Engagement-log plan entry — new subtask description (required for add, optional for modify). Written in the engagement language declared by your system prompt."`
}

type SubtaskInfoPatch struct {
	ID int64 `json:"id,omitempty" jsonschema:"title=Subtask ID" jsonschema_description:"ID of the subtask (populated by the system for existing subtasks)"`
	SubtaskInfo
}

// SubtaskPatch is the delta-based refinement output for modifying subtask lists
type SubtaskPatch struct {
	Operations SubtaskOperations `json:"operations" jsonschema:"required" jsonschema_description:"List of operations to apply to the current subtask list. Empty array means no changes needed. Each operation's title/description, when present, is an engagement-log plan entry (see operations)."`
	Message    string            `json:"message" jsonschema:"required,title=Refinement summary" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on the changes made and the justification for the modifications. Written in the engagement language declared by your system prompt."`
}

type TaskResult struct {
	// Success is a pointer so an omitted field is distinguishable from false.
	// It decides whether the task is recorded as finished or failed, and a model
	// that leaves it out must be asked again rather than have a failure assumed.
	Success *Bool  `json:"success" jsonschema:"title=Execution result,type=boolean" jsonschema_description:"True if the task was executed successfully and its objective was reached"`
	Result  string `json:"result" jsonschema:"required,title=Task result description" jsonschema_description:"Engagement-log closing entry — fully detailed write-up of the task/subtask outcome (what was achieved or why it failed). Written in the engagement language declared by your system prompt."`
	Message string `json:"message" jsonschema:"required,title=Task result message" jsonschema_description:"Engagement-log closing summary — a concise 1-2 sentence recap of the outcome and the path taken to reach the goal. Written in the engagement language declared by your system prompt."`
}

const (
	toolCallFieldEnd     = `</parameter>`
	toolCallMessageStart = `<parameter name="message">`
	toolCallFieldStart   = `<parameter name=`
)

// splitLeakedMessage recovers a message a model wrote inside the result after closing the field as markup.
// The message tag must follow the closing tag with only whitespace between them; anything else is a quotation.
func splitLeakedMessage(result, message string) (string, string) {
	if message != "" {
		return result, message
	}

	end := strings.Index(result, toolCallFieldEnd)
	if end < 0 {
		return result, message
	}

	rest := result[end+len(toolCallFieldEnd):]
	start := strings.Index(rest, toolCallMessageStart)
	if start < 0 || strings.TrimSpace(rest[:start]) != "" {
		return result, message
	}

	return strings.TrimRight(result[:end], " \t\r\n"), strings.TrimSpace(rest[start+len(toolCallMessageStart):])
}

func (t *TaskResult) UnmarshalJSON(data []byte) error {
	type plain TaskResult

	var parsed plain
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}

	*t = TaskResult(parsed)
	t.Result, t.Message = splitLeakedMessage(t.Result, t.Message)

	return nil
}

func (d *Done) UnmarshalJSON(data []byte) error {
	type plain Done

	var parsed plain
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}

	*d = Done(parsed)
	d.Result, d.Message = splitLeakedMessage(d.Result, d.Message)

	return nil
}

func (s *SearchResult) UnmarshalJSON(data []byte) error {
	type plain SearchResult

	var parsed plain
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}

	*s = SearchResult(parsed)
	s.Result, s.Message = splitLeakedMessage(s.Result, s.Message)

	return nil
}

type AskUser struct {
	Message string `json:"message" jsonschema:"required,title=Question for user" jsonschema_description:"Engagement-log entry — clarification question or any other information addressed to the engagement coordinator. Written in the engagement language declared by your system prompt."`
}

type Done struct {
	// Success is a pointer for the reason TaskResult.Success is.
	Success *Bool  `json:"success" jsonschema:"title=Execution result,type=boolean" jsonschema_description:"True if the subtask was executed successfully and its objective was reached"`
	Result  string `json:"result" jsonschema:"required,title=Subtask result description" jsonschema_description:"Engagement-log closing entry — fully detailed write-up of the subtask outcome (what was achieved or why it failed). Written in the engagement language declared by your system prompt."`
	Message string `json:"message" jsonschema:"required,title=Subtask result message" jsonschema_description:"Engagement-log closing summary — a concise 1-2 sentence recap of the subtask outcome. Written in the engagement language declared by your system prompt."`
}

type TerminalAction struct {
	Input   string `json:"input" jsonschema:"required" jsonschema_description:"Command to be run in the docker container terminal according to the command-execution rules"`
	Cwd     string `json:"cwd" jsonschema:"required" jsonschema_description:"Custom current working directory to execute the command in, or the default directory if not specified"`
	Detach  Bool   `json:"detach" jsonschema:"required,type=boolean" jsonschema_description:"Set to true for INTERACTIVE or LONG-RUNNING commands: shells (msfconsole, bash, python), listeners (nc -lvnp, socat TCP-LISTEN), servers (python -m http.server, php -S), monitors (tcpdump, tail -f). These commands expect user input or run indefinitely. When true: command runs in background, you get immediate confirmation, no stdout/stderr captured. When false: command must complete within timeout and return output. For quick batch commands (nmap, curl, ls) use false"`
	Timeout Int64  `json:"timeout" jsonschema:"required,type=integer" jsonschema_description:"Execution time limit in seconds. Use 0 value to apply the configured server default timeout. Explicit positive values are accepted up to 10800 seconds (3 hours); any value outside the 1–10800 range or non-positive is replaced by the server default. For batch commands that may run long, use the 'timeout' shell utility INSIDE your command to ensure clean completion with full output: 'timeout 55 nmap -sV target' (set 5-10 seconds less than this parameter). For interactive/long-running commands, use detach=true instead of relying solely on timeout"`
	Message string `json:"message" jsonschema:"required,title=Terminal command message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary explaining what you intend to achieve by running this command. Written in the engagement language declared by your system prompt."`
}

type AskAdvice struct {
	Question string `json:"question" jsonschema:"required" jsonschema_description:"Technical-channel payload — detailed question for the senior mentor describing the issue and what you need to figure out. Always written in English; never translated."`
	Code     string `json:"code" jsonschema_description:"Technical-channel excerpt — optional code snippet relevant to the question. Preserve verbatim; do not translate code or comments."`
	Output   string `json:"output" jsonschema_description:"Technical-channel excerpt — optional stdout/stderr excerpt relevant to the question. Preserve verbatim."`
	Message  string `json:"message" jsonschema:"required,title=Ask advice message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary explaining what you are asking the mentor and why. Written in the engagement language declared by your system prompt."`
}

type ComplexSearch struct {
	Question string `json:"question" jsonschema:"required" jsonschema_description:"Technical-channel payload — research question for the researcher specialist with full context, target content type, and required structure. Always written in English; never translated."`
	Message  string `json:"message" jsonschema:"required,title=Search query message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the research question. Written in the engagement language declared by your system prompt."`
}

type SearchAction struct {
	Query      string `json:"query" jsonschema:"required" jsonschema_description:"Technical-channel payload — query for the specific search engine (google, duckduckgo, tavily, traversaal, perplexity, serper, etc.). ALWAYS written in English regardless of the engagement language: internet sources are predominantly indexed in English and non-English queries return poor or empty results. Short and exact queries return better results."`
	MaxResults Int64  `json:"max_results" jsonschema:"required,type=integer" jsonschema_description:"Maximum number of results to return (minimum 1; maximum 10; default 5)"`
	Message    string `json:"message" jsonschema:"required,title=Search query message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary explaining the expected result and how it advances the goal. Written in the engagement language declared by your system prompt."`
}

type SearchResult struct {
	Result  string `json:"result" jsonschema:"required,title=Search result" jsonschema_description:"Technical-channel payload — fully detailed search report (or error explanation) returned to the calling agent for further reasoning. Always written in English; never translated."`
	Message string `json:"message" jsonschema:"required,title=Search result message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary with a short summary of the result. Written in the engagement language declared by your system prompt."`
}

type SploitusAction struct {
	Query       string `json:"query" jsonschema:"required" jsonschema_description:"Technical-channel payload — search query for Sploitus (e.g. 'ssh', 'apache 2.4', 'CVE-2021-44228'). ALWAYS written in English; the Sploitus index is English-only. Short and precise queries return the best results."`
	ExploitType String `json:"exploit_type,omitempty" jsonschema:"type=string,enum=exploits,enum=tools" jsonschema_description:"What to search for: 'exploits' (default) for exploit code and PoCs, 'tools' for offensive security tools"`
	Sort        String `json:"sort,omitempty" jsonschema:"type=string,enum=default,enum=date,enum=score" jsonschema_description:"Result ordering: 'default' (relevance), 'date' (newest first), 'score' (highest CVSS first)"`
	MaxResults  Int64  `json:"max_results" jsonschema:"required,type=integer" jsonschema_description:"Maximum number of results to return (minimum 1; maximum 25; default 10)"`
	Message     string `json:"message" jsonschema:"required,title=Search query message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary explaining the expected result and how it advances the goal. Written in the engagement language declared by your system prompt."`
}

// WebSearchAction is the LLM-facing schema for the unified web_search tool. The agent
// supplies a query and an intent `mode`; web_search selects the concrete engine,
// retries transient failures, and falls back across providers automatically.
type WebSearchAction struct {
	Query       string `json:"query" jsonschema:"required" jsonschema_description:"Technical-channel payload — the search query. ALWAYS written in English regardless of the engagement language: internet sources and the underlying engines are indexed in English and non-English queries return poor or empty results. Keep it short and keyword-focused."`
	Mode        String `json:"mode,omitempty" jsonschema:"type=string,enum=links,enum=answer,enum=research" jsonschema_description:"What you need back (default 'answer'). 'links' — a list of source links with snippets (fastest/cheapest). 'answer' — a synthesized answer over live sources. 'research' — deep multi-source analysis with reasoning (most thorough). web_search picks the concrete engine for you and falls back automatically; you do NOT name an engine."`
	MaxResults  Int64  `json:"max_results,omitempty" jsonschema:"type=integer" jsonschema_description:"Maximum number of results to return (1–25; default 5). Ignored by answer/research engines that return a single synthesized answer."`
	ExploitType String `json:"exploit_type,omitempty" jsonschema:"type=string,enum=exploits,enum=tools" jsonschema_description:"exploit mode only: 'exploits' (default) for exploit code and PoCs, 'tools' for offensive-security tools."`
	Sort        String `json:"sort,omitempty" jsonschema:"type=string,enum=default,enum=date,enum=score" jsonschema_description:"exploit mode only: result ordering — 'default' (relevance), 'date' (newest first), 'score' (highest CVSS first)."`
	Message     string `json:"message" jsonschema:"required,title=Search query message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary explaining the expected result and how it advances the goal. Written in the engagement language declared by your system prompt."`
}

type GraphitiSearchAction struct {
	SearchType     String   `json:"search_type" jsonschema:"required,type=string,enum=temporal_window,enum=entity_relationships,enum=diverse_results,enum=episode_context,enum=successful_tools,enum=recent_context,enum=entity_by_label" jsonschema_description:"Type of search to perform: temporal_window (time-bounded search), entity_relationships (graph traversal from an entity), diverse_results (anti-redundancy search), episode_context (full agent reasoning and tool outputs), successful_tools (proven techniques), recent_context (latest findings), entity_by_label (type-specific entity search, REQUIRES node_labels)"`
	Query          string   `json:"query" jsonschema:"required" jsonschema_description:"Technical-channel payload — natural language query against the team's temporal knowledge graph. REQUIRED for EVERY search_type, including entity_relationships/entity_by_label: it re-ranks/filters the graph traversal, it is never optional just because center_node_uuid or node_labels is set. ALWAYS written in English regardless of the engagement language: the graph is indexed in English and shared across all engagements; non-English queries will fail to retrieve stored episodic memory."`
	MaxResults     *Int64   `json:"max_results,omitempty" jsonschema:"title=Maximum Results,type=integer" jsonschema_description:"Maximum number of results to return (default varies by search type)"`
	TimeStart      string   `json:"time_start,omitempty" jsonschema_description:"Start of time window (ISO 8601 format, required for temporal_window)"`
	TimeEnd        string   `json:"time_end,omitempty" jsonschema_description:"End of time window (ISO 8601 format, required for temporal_window)"`
	CenterNodeUUID string   `json:"center_node_uuid,omitempty" jsonschema_description:"REQUIRED for entity_relationships. Copy verbatim from the 'UUID:' field of an entity/community returned by an EARLIER graphiti_search call in this conversation (any search_type). NEVER invent one — not a flow/task ID, hostname, or title; if no prior result yielded a UUID, run recent_context/entity_by_label first"`
	MaxDepth       *Int64   `json:"max_depth,omitempty" jsonschema:"title=Maximum Depth,type=integer" jsonschema_description:"Maximum graph traversal depth (default: 2, max: 3, for entity_relationships)"`
	NodeLabels     []string `json:"node_labels,omitempty" jsonschema_description:"Filter to specific node types — EXACT taxonomy names, PascalCase singular (e.g., ['Host', 'Service', 'Vulnerability']). REQUIRED (non-empty) when search_type is entity_by_label; optional filter otherwise"`
	EdgeTypes      []string `json:"edge_types,omitempty" jsonschema_description:"Filter to specific relationship types — EXACT taxonomy names, UPPER_SNAKE_CASE (e.g., ['HAS_PORT', 'HAS_VULNERABILITY'])"`
	DiversityLevel String   `json:"diversity_level,omitempty" jsonschema:"type=string,enum=low,enum=medium,enum=high" jsonschema_description:"How much diversity to prioritize (default: medium, for diverse_results)"`
	MinMentions    *Int64   `json:"min_mentions,omitempty" jsonschema:"title=Minimum Mentions,type=integer" jsonschema_description:"Minimum episode mentions (default: 2, for successful_tools)"`
	RecencyWindow  String   `json:"recency_window,omitempty" jsonschema:"type=string,enum=1h,enum=6h,enum=24h,enum=7d" jsonschema_description:"How far back to search (default: 24h, for recent_context)"`
	Message        string   `json:"message" jsonschema:"required,title=Search message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the search query and expected results. Written in the engagement language declared by your system prompt."`
}

type EnricherResult struct {
	Result  string `json:"result" jsonschema:"required,title=Enricher result" jsonschema_description:"Technical-channel payload — fully detailed enrichment report (or error explanation) returned to the calling agent for further reasoning. Always written in English; never translated."`
	Message string `json:"message" jsonschema:"required,title=Enricher result message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary with a short view of the enriched data. Written in the engagement language declared by your system prompt."`
}

type MemoristAction struct {
	Question  string `json:"question" jsonschema:"required" jsonschema_description:"Technical-channel payload — question for complex search in previous work, tasks, and calls; include full context of what happened and what you want to find. Always written in English; never translated."`
	TaskID    *Int64 `json:"task_id,omitempty" jsonschema:"title=Task ID,type=integer" jsonschema_description:"If you know task id you can use it to get more relevant information from the vector database; it will be used as a hard filter for search (optional)"`
	SubtaskID *Int64 `json:"subtask_id,omitempty" jsonschema:"title=Subtask ID,type=integer" jsonschema_description:"If you know subtask id you can use it to get more relevant information from the vector database; it will be used as a hard filter for search (optional)"`
	Message   string `json:"message" jsonschema:"required,title=Search message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the question and the path to reach the goal. Written in the engagement language declared by your system prompt."`
}

type MemoristResult struct {
	Result  string `json:"result" jsonschema:"required,title=Search in long-term memory result" jsonschema_description:"Technical-channel payload — fully detailed long-term memory search report (or error explanation) returned to the calling agent for further reasoning. Always written in English; never translated."`
	Message string `json:"message" jsonschema:"required,title=Search in long-term memory result message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary with a short answer summary. Written in the engagement language declared by your system prompt."`
}

type SearchInMemoryAction struct {
	Questions Strings `json:"questions" jsonschema:"required,type=array,minItems=1,maxItems=5" jsonschema_description:"Technical-channel payload — 1 to 5 detailed, context-rich semantic queries against the team's long-term vector store. Must be a real JSON array of strings, e.g. [\"query 1\",\"query 2\"] - NOT a JSON-encoded string containing an array. ALWAYS written in English regardless of the engagement language: the store is indexed in English and shared across all engagements, so non-English queries will fail to retrieve relevant stored knowledge. Each query should provide context, intent, and specific details with descriptive phrases, synonyms, and related terms; multiple queries explore different semantic angles. Note: If TaskID or SubtaskID are provided, they will be used as strict filters in the search."`
	TaskID    *Int64  `json:"task_id,omitempty" jsonschema:"title=Task ID" jsonschema_description:"Optional. The Task ID to use as a strict filter, retrieving information specifically related to this task. Used to enhance relevance by narrowing down the search scope. Type: integer."`
	SubtaskID *Int64  `json:"subtask_id,omitempty" jsonschema:"title=Subtask ID" jsonschema_description:"Optional. The Subtask ID to use as a strict filter, retrieving information specifically related to this subtask. Helps in refining search results for increased relevancy. Type: integer."`
	Message   string  `json:"message" jsonschema:"required,title=Search-in-memory message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the queries or the information retrieval process. Written in the engagement language declared by your system prompt."`
}

type SearchGuideAction struct {
	Questions Strings   `json:"questions" jsonschema:"required,type=array,minItems=1,maxItems=5" jsonschema_description:"Technical-channel payload — 1 to 5 detailed, context-rich semantic queries for the team's guide vector store. Must be a real JSON array of strings, e.g. [\"query 1\",\"query 2\"] - NOT a JSON-encoded string containing an array. ALWAYS written in English regardless of the engagement language: the store is indexed in English and shared across all engagements, so non-English queries will fail to retrieve relevant guides. Each query should include scenario context, objectives, and specific intent. Note: The 'Type' field acts as a strict filter."`
	Type      GuideType `json:"type" jsonschema:"required,type=string,enum=install,enum=configure,enum=use,enum=pentest,enum=development,enum=other" jsonschema_description:"The specific type of guide you need. This required field acts as a strict filter to enhance the relevance of search results by narrowing down the scope to the specified guide type."`
	Message   string    `json:"message" jsonschema:"required,title=Guide search message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the queries and the type of guide needed. Written in the engagement language declared by your system prompt."`
}

type StoreGuideAction struct {
	Guide    string    `json:"guide" jsonschema:"required" jsonschema_description:"Technical-channel payload — ready guide in markdown format that will be stored in the team's vector store for future retrieval. ALWAYS written in English regardless of the engagement language: the store is indexed in English and shared across all engagements; non-English content becomes unreachable to future searches. Anonymize all sensitive data (IPs, domains, credentials, paths) using descriptive placeholders."`
	Question string    `json:"question" jsonschema:"required" jsonschema_description:"Technical-channel payload — question that was used to prepare this guide; co-indexed with the guide. Always written in English; never translated."`
	Type     GuideType `json:"type" jsonschema:"required,type=string,enum=install,enum=configure,enum=use,enum=pentest,enum=development,enum=other" jsonschema_description:"Type of the guide to store; it will be used as a hard filter for search"`
	Message  string    `json:"message" jsonschema:"required,title=Store guide message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the guide. Written in the engagement language declared by your system prompt."`
}

type SearchAnswerAction struct {
	Questions Strings    `json:"questions" jsonschema:"required,type=array,minItems=1,maxItems=5" jsonschema_description:"Technical-channel payload — 1 to 5 detailed, context-rich semantic queries for the team's answer vector store. Must be a real JSON array of strings, e.g. [\"query 1\",\"query 2\"] - NOT a JSON-encoded string containing an array. ALWAYS written in English regardless of the engagement language: the store is indexed in English and shared across all engagements, so non-English queries will fail to retrieve relevant answers. Each query should include the context, what you want to find, what you intend to do with the information, and why you need it. Note: The 'Type' field acts as a strict filter."`
	Type      AnswerType `json:"type" jsonschema:"required,type=string,enum=guide,enum=vulnerability,enum=code,enum=tool,enum=other" jsonschema_description:"The specific type of information or answer you are seeking. This required field acts as a strict filter to enhance the relevance of search results by narrowing down the scope to the specified type."`
	Message   string     `json:"message" jsonschema:"required,title=Answer search message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the queries and the type of answer needed. Written in the engagement language declared by your system prompt."`
}

type StoreAnswerAction struct {
	Answer   string     `json:"answer" jsonschema:"required" jsonschema_description:"Technical-channel payload — ready answer in markdown format that will be stored in the team's vector store for future retrieval. ALWAYS written in English regardless of the engagement language: the store is indexed in English and shared across all engagements; non-English content becomes unreachable to future searches. Anonymize all sensitive data (IPs, domains, credentials) using descriptive placeholders."`
	Question string     `json:"question" jsonschema:"required" jsonschema_description:"Technical-channel payload — question that was used to prepare this answer; co-indexed with the answer. Always written in English; never translated."`
	Type     AnswerType `json:"type" jsonschema:"required,type=string,enum=guide,enum=vulnerability,enum=code,enum=tool,enum=other" jsonschema_description:"Type of the search query and answer to store; it will be used as a hard filter for search"`
	Message  string     `json:"message" jsonschema:"required,title=Store answer message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the answer. Written in the engagement language declared by your system prompt."`
}

type SearchCodeAction struct {
	Questions Strings  `json:"questions" jsonschema:"required,type=array,minItems=1,maxItems=5" jsonschema_description:"Technical-channel payload — 1 to 5 detailed, context-rich semantic queries for the team's code vector store. Must be a real JSON array of strings, e.g. [\"query 1\",\"query 2\"] - NOT a JSON-encoded string containing an array. ALWAYS written in English regardless of the engagement language: the store is indexed in English and shared across all engagements, so non-English queries will fail to retrieve relevant code samples. Each query should include the context, what you intend to achieve with the code, and the functionality or content that should be included."`
	Lang      CodeLang `json:"lang" jsonschema:"required" jsonschema_description:"The programming language of the code samples you need. Use the standard markdown code block language name (e.g., 'python', 'bash', 'golang'). This required field narrows down the search to code samples in the desired language."`
	Message   string   `json:"message" jsonschema:"required,title=Code search message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the queries and the programming language of the code samples. Written in the engagement language declared by your system prompt."`
}

type StoreCodeAction struct {
	Code        string   `json:"code" jsonschema:"required" jsonschema_description:"Ready code sample that will be stored for future retrieval (raw source code, not a localized message). Anonymize all sensitive data (IPs, domains, credentials, API keys) using descriptive placeholders."`
	Question    string   `json:"question" jsonschema:"required" jsonschema_description:"Technical-channel payload — question that was used to prepare or to write this code; co-indexed with the code in the team's vector store. Always written in English; never translated."`
	Lang        CodeLang `json:"lang" jsonschema:"required" jsonschema_description:"Programming language of the code sample; use markdown code block language name like python or bash or golang etc."`
	Explanation string   `json:"explanation" jsonschema:"required" jsonschema_description:"Technical-channel payload — fully detailed explanation of the code sample (what it does, how it works, why it is useful, libraries/tools used). ALWAYS written in English; the explanation is co-indexed with the code in the team's vector store and shared across all engagements; non-English content becomes unreachable to future searches."`
	Description string   `json:"description" jsonschema:"required" jsonschema_description:"Technical-channel payload — short description of the code sample as a summary of the explanation; co-indexed in the team's vector store. Always written in English; never translated."`
	Message     string   `json:"message" jsonschema:"required,title=Store code message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the code sample. Written in the engagement language declared by your system prompt."`
}

type MaintenanceAction struct {
	Question string `json:"question" jsonschema:"required" jsonschema_description:"Technical-channel payload — task description for the DevOps team member to maintain the local environment and tools inside the docker container. Always written in English; never translated."`
	Message  string `json:"message" jsonschema:"required,title=Maintenance task message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the maintenance task. Written in the engagement language declared by your system prompt."`
}

type MaintenanceResult struct {
	Result  string `json:"result" jsonschema:"required,title=Maintenance result description" jsonschema_description:"Technical-channel payload — fully detailed maintenance report (or error explanation) returned to the calling agent, with usage instructions for the result. Always written in English; never translated."`
	Message string `json:"message" jsonschema:"required,title=Maintenance result message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary with the result and the path to reach the goal. Written in the engagement language declared by your system prompt."`
}

type CoderAction struct {
	Question string `json:"question" jsonschema:"required" jsonschema_description:"Technical-channel payload — task description for the developer team member to write code, with detailed explanation of the goal and how to achieve it when not obvious. Always written in English; never translated."`
	Message  string `json:"message" jsonschema:"required,title=Coder action message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the question and the task. Written in the engagement language declared by your system prompt."`
}

type CodeResult struct {
	Result  string `json:"result" jsonschema:"required,title=Code result description" jsonschema_description:"Technical-channel payload — fully detailed code-development report (or error explanation) returned to the calling agent, with usage instructions for the result. Always written in English; never translated."`
	Message string `json:"message" jsonschema:"required,title=Code result message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary with the result and the path to reach the goal. Written in the engagement language declared by your system prompt."`
}

type PentesterAction struct {
	Question string `json:"question" jsonschema:"required" jsonschema_description:"Technical-channel payload — task description for the lead research specialist: the business profile, the sources to read, the remaining gap to the target, and the expected output. Always written in English; never translated."`
	Message  string `json:"message" jsonschema:"required,title=Pentester action message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary summarizing the question and the task. Written in the engagement language declared by your system prompt."`
}

type HackResult struct {
	Result  string `json:"result" jsonschema:"required,title=Lead research result description" jsonschema_description:"Technical-channel payload — fully detailed lead research report (or error explanation) returned to the calling agent, with usage instructions for the result. Always written in English; never translated."`
	Message string `json:"message" jsonschema:"required,title=Hack result message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary with the result and the path to reach the goal. Written in the engagement language declared by your system prompt."`
}

// FlowStatusDetail controls the level of detail returned by get_flow_status.
// It is a type alias for String - see the FileOp comment above.
type FlowStatusDetail = String

const (
	FlowStatusDetailSummary  FlowStatusDetail = "summary"
	FlowStatusDetailTasks    FlowStatusDetail = "tasks"
	FlowStatusDetailSubtasks FlowStatusDetail = "subtasks"
	FlowStatusDetailRunning  FlowStatusDetail = "running"
	FlowStatusDetailPlanned  FlowStatusDetail = "planned"
)

// GetFlowStatusAction defines arguments for the get_flow_status tool.
type GetFlowStatusAction struct {
	Detail  FlowStatusDetail `json:"detail" jsonschema:"required,type=string,enum=summary,enum=tasks,enum=subtasks,enum=running,enum=planned" jsonschema_description:"Level of detail: 'summary' - flow health snapshot with status and counts; 'tasks' - all tasks with ID/status/title; 'subtasks' - all subtasks optionally filtered by task_id; 'running' - full Task→Subtask execution chain including task input and recent agent messages; 'planned' - only subtasks with status 'created' (not yet started), optionally filtered by task_id"`
	TaskID  *Int64           `json:"task_id,omitempty" jsonschema:"title=Task ID,type=integer" jsonschema_description:"Optional task ID filter. Applies to detail=subtasks and detail=planned to narrow results to a specific task."`
	Verbose Bool             `json:"verbose,omitempty" jsonschema:"type=boolean" jsonschema_description:"Set to true for deeper investigation: includes descriptions, inputs, results, and execution context per entry; shows up to 50 recent agent messages instead of 10."`
	Message string           `json:"message" jsonschema:"required,title=Status message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary describing what status information you are requesting. Written in the engagement language declared by your system prompt."`
}

// StopFlowAction defines arguments for the stop_flow tool.
type StopFlowAction struct {
	Reason  string `json:"reason" jsonschema:"required" jsonschema_description:"Engagement-log entry — brief explanation of why the engagement is being halted; surfaced in the engagement record. Written in the engagement language declared by your system prompt."`
	Message string `json:"message" jsonschema:"required,title=Stop message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary explaining that the automation is being stopped. Written in the engagement language declared by your system prompt."`
}

// SubmitFlowInputAction defines arguments for the submit_flow_input tool.
type SubmitFlowInputAction struct {
	Input   string `json:"input" jsonschema:"required" jsonschema_description:"Engagement-log entry submitted on behalf of the engagement coordinator into the running automation. If a subtask is waiting at an 'ask' checkpoint, this is delivered as the coordinator's answer and execution resumes. If the flow is waiting with no active subtask, this becomes the goal for a new task — include complete context, targets, and constraints because the generator will decompose it into subtasks without further clarification. Written in the engagement language declared by your system prompt so downstream agents and the engagement record stay consistent."`
	Message string `json:"message" jsonschema:"required,title=Input message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary describing what is being submitted to the automation. Written in the engagement language declared by your system prompt."`
}

// WaitFlowCompletionAction defines arguments for the wait_flow_completion tool.
type WaitFlowCompletionAction struct {
	Timeout Int64  `json:"timeout" jsonschema:"required,type=integer" jsonschema_description:"Accepted and ignored. The wait blocks until the running automation task finishes, or for one hour if it does not. Pass 0."`
	Message string `json:"message" jsonschema:"required,title=Wait message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary explaining why you are waiting for the automation to finish. Written in the engagement language declared by your system prompt."`
}

// PatchFlowSubtasksAction defines arguments for the patch_flow_subtasks tool.
type PatchFlowSubtasksAction struct {
	TaskID     int64             `json:"task_id" jsonschema:"required,type=integer" jsonschema_description:"ID of the task whose subtask plan to modify. Obtain this from get_flow_status with detail='tasks'."`
	Operations SubtaskOperations `json:"operations" jsonschema:"required" jsonschema_description:"Delta operations to apply: add (insert new subtask at a position), remove (delete by ID), modify (update title/description), reorder (move to different position). Empty array returns the current plan unchanged. Each operation's title/description, when present, is an engagement-log plan entry (see operations)."`
	Message    string            `json:"message" jsonschema:"required,title=Patch summary" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary describing what changes are being made to the plan. Written in the engagement language declared by your system prompt."`
}

// ValidateSubtaskPatch validates the operations in a SubtaskPatch
// Validate reports an omitted success flag. Decoding cannot: a missing JSON
// field leaves the zero value and returns no error, which would record finished
// work as failed. The message names the field so the tool-call fixer can ask
// for it again.
func (tr TaskResult) Validate() error {
	if tr.Success == nil {
		return fmt.Errorf("success is required: state explicitly whether the objective was reached")
	}

	return nil
}

func (d Done) Validate() error {
	if d.Success == nil {
		return fmt.Errorf("success is required: state explicitly whether the objective was reached")
	}

	return nil
}

func (sp SubtaskPatch) Validate() error {
	for i, op := range sp.Operations {
		switch op.Op {
		case SubtaskOpAdd:
			if op.Title == "" {
				return fmt.Errorf("operation %d: add requires title", i)
			}
			if op.Description == "" {
				return fmt.Errorf("operation %d: add requires description", i)
			}
		case SubtaskOpRemove:
			if op.ID == nil {
				return fmt.Errorf("operation %d: remove requires id", i)
			}
		case SubtaskOpModify:
			if op.ID == nil {
				return fmt.Errorf("operation %d: modify requires id", i)
			}
			if op.Title == "" && op.Description == "" {
				return fmt.Errorf("operation %d: modify requires at least title or description", i)
			}
		case SubtaskOpReorder:
			if op.ID == nil {
				return fmt.Errorf("operation %d: reorder requires id", i)
			}
		default:
			return fmt.Errorf("operation %d: unknown operation type %q", i, op.Op)
		}
	}
	return nil
}

// DoubleEncodedListError is a list argument the model sent as a JSON STRING
// containing JSON, whose contents then failed to parse.
//
// Typed so a caller can tell this apart from a schema violation: the outer
// document conforms, the field really is a string, and what is wrong is one
// level of nesting. Nothing branches on the type today -- the fixer receives
// only the message -- so it is the message that has to carry that.
type DoubleEncodedListError struct {
	// Field is the argument that arrived wrapped, so the refusal can name it.
	Field string
	// Cause is what the unwrapped text failed on, kept for the log.
	Cause error
}

func (e *DoubleEncodedListError) Error() string {
	return fmt.Sprintf(
		"%s arrived as a string containing JSON, and that JSON does not parse: %s",
		e.Field, e.Cause,
	)
}

func (e *DoubleEncodedListError) Unwrap() error { return e.Cause }

// SubtaskInfos is []SubtaskInfo that also accepts the array as a JSON string,
// for the same reason Bool accepts a quoted boolean: models send it that way
// often enough to matter.
//
// The generator gets ONE attempt -- its chain is not a turn loop -- so a plan
// refused on its quotation marks is a task that never exists. A plan wrapped in
// a string is a serialisation slip, not a wrong plan.
type SubtaskInfos []SubtaskInfo

func (l *SubtaskInfos) UnmarshalJSON(data []byte) error {
	// A bare JSON "null" unmarshals into a nil slice with no error by default,
	// which would silently mask a required field being omitted - treat it as
	// invalid instead, consistent with Strings above.
	if trimmed := strings.TrimSpace(string(data)); trimmed == "null" {
		return fmt.Errorf("invalid subtasks value: expected a JSON array, got: null")
	}

	var direct []SubtaskInfo

	// The error from the ordinary path is what the caller needs when the payload
	// is an array whose CONTENTS are wrong: it names the offending element and
	// field. Reporting "must be a list" for those would contradict the arguments
	// logged beside it, and that text is what the tool-call fixer is given.
	directErr := json.Unmarshal(data, &direct)
	if directErr == nil {
		*l = direct

		return nil
	}

	return unwrapStringEncodedList(data, "subtasks", directErr, (*[]SubtaskInfo)(l))
}

// SubtaskOperations is the same leniency for the refiner's operation list.
type SubtaskOperations []SubtaskOperation

func (o *SubtaskOperations) UnmarshalJSON(data []byte) error {
	// A bare JSON "null" unmarshals into a nil slice with no error by default,
	// which would silently mask a required field being omitted - treat it as
	// invalid instead, consistent with Strings above.
	if trimmed := strings.TrimSpace(string(data)); trimmed == "null" {
		return fmt.Errorf("invalid operations value: expected a JSON array, got: null")
	}

	var direct []SubtaskOperation

	// The error from the ordinary path is what the caller needs when the payload
	// is an array whose CONTENTS are wrong: it names the offending element and
	// field. Reporting "must be a list" for those would contradict the arguments
	// logged beside it, and that text is what the tool-call fixer is given.
	directErr := json.Unmarshal(data, &direct)
	if directErr == nil {
		*o = direct

		return nil
	}

	return unwrapStringEncodedList(data, "operations", directErr, (*[]SubtaskOperation)(o))
}

// unwrapStringEncodedList is the tail both lenient list unmarshallers share: the
// payload is known not to parse as an array, so the remaining possibilities are
// a list wrapped in a JSON string, leaked tool-call markup, or a genuinely wrong
// argument. directErr is what to report when the value is not a string at all.
func unwrapStringEncodedList[T any](data []byte, field string, directErr error, out *[]T) error {
	var wrapped string
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return directErr
	}

	wrapped = strings.TrimSpace(wrapped)
	if wrapped == "null" {
		return fmt.Errorf("invalid %s value: expected a JSON array, got: null", field)
	}
	if strings.Contains(wrapped, toolCallFieldStart) {
		return &LeakedMarkupListError{Field: field}
	}

	var direct []T
	err := json.Unmarshal([]byte(wrapped), &direct)
	if err == nil {
		*out = direct

		return nil
	}

	// Escaping for one nesting level instead of two is a serialisation slip, not
	// a wrong plan, and the generator gets one attempt. Repair before refusing.
	if repaired := repairJSONStringEscapes(wrapped); repaired != wrapped {
		if json.Unmarshal([]byte(repaired), &direct) == nil {
			*out = direct

			return nil
		}
	}

	return &DoubleEncodedListError{Field: field, Cause: err}
}

// repairJSONStringEscapes doubles a backslash that begins no legal JSON escape.
// A model that escaped for one nesting level instead of two sends a literal
// `FLAG\{...\}`, `\xff` or `C:\users`; JSON rejects all three, and the
// backslash is text the model meant to keep. A trailing `\` before the closing
// quote is the same slip and is what turns the next quote into "invalid
// character '\"' after object key:value pair".
//
// Only run this on text a first parse has already rejected: doubling inside a
// payload that was correct would corrupt it.
func repairJSONStringEscapes(s string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/8)

	inString := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inString {
			if c == '"' {
				inString = true
			}
			b.WriteByte(c)

			continue
		}
		if c == '"' {
			inString = false
			b.WriteByte(c)

			continue
		}
		if c != '\\' {
			b.WriteByte(c)

			continue
		}
		if i+1 >= len(s) {
			b.WriteString(`\\`)

			continue
		}

		switch next := s[i+1]; next {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			b.WriteByte(c)
			b.WriteByte(next)
			i++
		case 'u':
			if i+6 <= len(s) && isHex4(s[i+2:i+6]) {
				b.WriteString(s[i : i+6])
				i += 5

				continue
			}
			b.WriteString(`\\`)
		default:
			b.WriteString(`\\`)
		}
	}

	return b.String()
}

func isHex4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}

	return true
}

// LeakedMarkupListError is a list argument whose value is the model's own
// tool-call markup rather than data -- Anthropic's `<parameter name="...">`
// emitted inside the JSON string. Nothing in it is a recoverable list, so the
// message has to tell the fixer what shape to send instead of what failed to
// parse.
type LeakedMarkupListError struct {
	// Field is the argument that arrived as markup, so the refusal can name it.
	Field string
}

func (e *LeakedMarkupListError) Error() string {
	return fmt.Sprintf(
		"%s contains tool-call markup instead of data: send %s as a JSON array of objects, "+
			"not as text containing <parameter name=\"...\"> tags",
		e.Field, e.Field,
	)
}

type Bool bool

func (b *Bool) UnmarshalJSON(data []byte) error {
	sdata := strings.Trim(strings.ToLower(string(data)), "' \"\n\r\t")
	switch sdata {
	case "true":
		*b = true
	case "false":
		*b = false
	default:
		return fmt.Errorf("invalid bool value: %s", sdata)
	}
	return nil
}

func (b *Bool) MarshalJSON() ([]byte, error) {
	if b == nil || !*b {
		return []byte("false"), nil
	}
	return []byte("true"), nil
}

func (b *Bool) Bool() bool {
	if b == nil {
		return false
	}
	return bool(*b)
}

func (b *Bool) String() string {
	if b == nil {
		return ""
	}
	return strconv.FormatBool(bool(*b))
}

type Int64 int64

func (i *Int64) UnmarshalJSON(data []byte) error {
	sdata := strings.Trim(strings.ToLower(string(data)), "' \"\n\r\t")
	if sdata == "" {
		*i = 0
		return nil
	}
	num, err := strconv.ParseInt(sdata, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid int value: %s", sdata)
	}
	*i = Int64(num)
	return nil
}

func (i *Int64) MarshalJSON() ([]byte, error) {
	if i == nil {
		return []byte("0"), nil
	}
	return []byte(strconv.FormatInt(int64(*i), 10)), nil
}

func (i *Int64) Int() int {
	if i == nil {
		return 0
	}
	return int(*i)
}

func (i *Int64) Int64() int64 {
	if i == nil {
		return 0
	}
	return int64(*i)
}

func (i *Int64) PtrInt64() *int64 {
	if i == nil {
		return nil
	}
	v := int64(*i)
	return &v
}

func (i *Int64) String() string {
	if i == nil {
		return ""
	}
	return strconv.FormatInt(int64(*i), 10)
}

// String is a lenient string for LLM-generated tool-call arguments, in
// particular enum-like fields (action/type/search_type/... - anywhere a
// value is compared or switched on rather than read as free text). Models
// occasionally wrap the value in an extra, literal pair of quote characters
// - e.g. the JSON string "\"write_file\"" decodes via a plain string
// UnmarshalJSON into the 12-character Go string `"write_file"` (quotes
// included), which then matches no known enum value, fails validation, and
// forces an unnecessary tool-call-fixer round-trip. UnmarshalJSON unwraps
// that redundant quoting instead of failing outright.
//
// FileOp, BrowserAction, SubtaskOperationType, and FlowStatusDetail are type
// aliases for String (not distinct types), so every field declared with one
// of those names gets this leniency automatically, with zero changes needed
// at any existing call site: switches, `==` comparisons, and assignments
// against their constants all keep working exactly as before, because the
// alias makes them the exact same type as String, not merely a similar one.
type String string

func (s *String) UnmarshalJSON(data []byte) error {
	// A bare JSON "null" unmarshals into an empty string with no error by
	// default, which would silently mask a required field being omitted -
	// treat it as invalid instead, consistent with Bool/Int64/Strings above.
	if trimmed := strings.TrimSpace(string(data)); trimmed == "null" {
		return fmt.Errorf("invalid string value: got null")
	}

	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = String(unwrapRedundantQuotes(raw))
	return nil
}

func (s String) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// String implements fmt.Stringer, so %s/%v formatting and logrus fields show
// the unwrapped value, and callers that need a plain string (map keys,
// strings.* helpers, external struct fields, typed casts) can call it explicitly.
func (s String) String() string {
	return string(s)
}

// unwrapRedundantQuotes strips at most a few layers of a matching leading+
// trailing '"' pair from a decoded JSON string value. Bounded to guard
// against pathological input; the observed corruption is a single extra
// layer, but a small bound costs nothing and covers repeated wrapping too.
func unwrapRedundantQuotes(s string) string {
	for range 3 {
		trimmed := strings.TrimSpace(s)
		if len(trimmed) < 2 || trimmed[0] != '"' || trimmed[len(trimmed)-1] != '"' {
			return s
		}
		s = trimmed[1 : len(trimmed)-1]
	}
	return s
}

// Strings is a lenient []string for LLM-generated tool-call arguments (e.g.
// the "questions" field of vector-store search tools). Models occasionally
// double-encode the array as a JSON string containing the array literal
// (e.g. "[\"a\", \"b\"]" instead of a real ["a","b"]), which fails a plain
// []string unmarshal with "cannot unmarshal string into ... []string" and
// forces an unnecessary tool-call-fixer round-trip. UnmarshalJSON recovers
// from that case instead of failing outright.
type Strings []string

func (s *Strings) UnmarshalJSON(data []byte) error {
	// A bare JSON "null" unmarshals into a nil slice with no error by default,
	// which would silently mask a required field being omitted - treat it as
	// invalid instead, consistent with Bool/Int64 above.
	if trimmed := strings.TrimSpace(string(data)); trimmed == "null" {
		return fmt.Errorf("invalid strings value: expected a JSON array of strings, got: null")
	}

	// Primary path: a real JSON array of strings - the common, schema-conforming case.
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*s = arr
		return nil
	}

	// Recovery path: the array was sent as a JSON string. Try to decode its
	// content as a JSON array of strings first (the double-encoding case seen
	// in production), then fall back to treating the whole string as a single
	// element so one plain question doesn't fail either.
	var encoded string
	if err := json.Unmarshal(data, &encoded); err == nil {
		var nested []string
		if err := json.Unmarshal([]byte(encoded), &nested); err == nil {
			*s = nested
			return nil
		}
		if trimmed := strings.TrimSpace(encoded); trimmed != "" {
			*s = []string{trimmed}
			return nil
		}
	}

	return fmt.Errorf(
		"invalid strings value: expected a JSON array of strings, e.g. [\"question 1\",\"question 2\"], got: %s",
		strings.TrimSpace(string(data)),
	)
}

func (s *Strings) MarshalJSON() ([]byte, error) {
	if s == nil || *s == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]string(*s))
}

// GuideType is a KnowledgeGuideType decoded from LLM-generated tool-call arguments.
type GuideType model.KnowledgeGuideType

func (t *GuideType) UnmarshalJSON(data []byte) error {
	var raw String
	if err := raw.UnmarshalJSON(data); err != nil {
		return err
	}
	gt := model.KnowledgeGuideType(strings.ToLower(strings.TrimSpace(raw.String())))
	if !gt.IsValid() {
		gt = model.KnowledgeGuideTypeOther
	}
	*t = GuideType(gt)
	return nil
}

type CodeLang string

func (l *CodeLang) UnmarshalJSON(data []byte) error {
	var raw String
	if err := raw.UnmarshalJSON(data); err != nil {
		return err
	}
	*l = CodeLang(limits.CanonicalCodeLang(raw.String()))

	return nil
}

func (l CodeLang) MarshalJSON() ([]byte, error) { return json.Marshal(string(l)) }

func (l CodeLang) String() string { return string(l) }

func (t GuideType) MarshalJSON() ([]byte, error) { return json.Marshal(string(t)) }

func (t GuideType) String() string { return string(t) }

// AnswerType is the KnowledgeAnswerType counterpart of GuideType.
type AnswerType model.KnowledgeAnswerType

func (t *AnswerType) UnmarshalJSON(data []byte) error {
	var raw String
	if err := raw.UnmarshalJSON(data); err != nil {
		return err
	}
	at := model.KnowledgeAnswerType(strings.ToLower(strings.TrimSpace(raw.String())))
	if !at.IsValid() {
		at = model.KnowledgeAnswerTypeOther
	}
	*t = AnswerType(at)
	return nil
}

func (t AnswerType) MarshalJSON() ([]byte, error) { return json.Marshal(string(t)) }

func (t AnswerType) String() string { return string(t) }

// IDString is an identifier models send either as a JSON string or as a bare number
// (Telegram ids, message ids); both decode to the same text.
type IDString string

func (s *IDString) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" || trimmed == "" {
		*s = ""
		return nil
	}
	if trimmed[0] != '"' {
		var num json.Number
		if err := json.Unmarshal(data, &num); err != nil {
			return fmt.Errorf("invalid id value: expected a string or a number, got: %s", trimmed)
		}
		*s = IDString(num.String())
		return nil
	}
	var raw String
	if err := raw.UnmarshalJSON(data); err != nil {
		return err
	}
	*s = IDString(strings.TrimSpace(raw.String()))
	return nil
}

func (s IDString) String() string { return string(s) }

type GrowthSearchSourcesAction struct {
	Mode     String  `json:"mode" jsonschema:"required,type=string,enum=search,enum=resolve,enum=recommend" jsonschema_description:"'search' finds public Telegram groups and channels by keyword (query). 'resolve' turns links found elsewhere (Telegram links, @usernames, or forum and web page URLs) into pool sources (links). 'recommend' lists Telegram channels similar to a pool source (source_id). Every source found is added to the source pool."`
	Query    string  `json:"query,omitempty" jsonschema_description:"search only: keywords in the language the target audience writes in, e.g. a product name or category. Short queries return more; rotate several."`
	Links    Strings `json:"links,omitempty" jsonschema_description:"resolve only: links to add, e.g. https://t.me/somegroup, @somechannel, https://forum.example.com/thread/123"`
	SourceID Int64   `json:"source_id,omitempty" jsonschema:"type=integer" jsonschema_description:"recommend only: the pool source_id to find similar channels for"`
	Limit    Int64   `json:"limit,omitempty" jsonschema:"type=integer" jsonschema_description:"Maximum results (1-50, default 20)"`
	Message  string  `json:"message" jsonschema:"required,title=Source search message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on what you are looking for and why. Written in the engagement language declared by your system prompt."`
}

type GrowthAddSourcesAction struct {
	Links                  Strings `json:"links" jsonschema:"required" jsonschema_description:"Telegram links, @usernames, invite links, or forum and web page URLs: supplied by the customer, or found inside a source's messages. Duplicates of sources already in the pool are reported as existing."`
	DiscoveredFromSourceID *Int64  `json:"discovered_from_source_id,omitempty" jsonschema:"type=integer" jsonschema_description:"When the links were found in the messages of a pool source, that source_id; omit for links the customer supplied"`
	Message                string  `json:"message" jsonschema:"required,title=Add sources message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on which sources are being added. Written in the engagement language declared by your system prompt."`
}

type GrowthJoinSourceAction struct {
	SourceIDs []Int64 `json:"source_ids" jsonschema:"required" jsonschema_description:"Pool source_ids to join with the customer's Telegram account. Already joined sources are skipped; web sources need no joining."`
	Message   string  `json:"message" jsonschema:"required,title=Join sources message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on which sources are being joined and why. Written in the engagement language declared by your system prompt."`
}

type GrowthSyncSourceAction struct {
	Action         String   `json:"action" jsonschema:"required,type=string,enum=history,enum=user" jsonschema_description:"'history' reads the next page of a Telegram source's public messages (source_id). 'user' returns the public profile of a Telegram user (platform_user_id)."`
	SourceID       Int64    `json:"source_id,omitempty" jsonschema:"type=integer" jsonschema_description:"history only: the pool source_id to read"`
	SourceIDs      []Int64  `json:"source_ids,omitempty" jsonschema:"type=array" jsonschema_description:"history only: several pool source_ids to read in one call, instead of source_id. With neither, every source an account has joined is read in turn"`
	Direction      String   `json:"direction,omitempty" jsonschema:"type=string,enum=older,enum=newer" jsonschema_description:"history only: 'older' (default) continues backwards from the oldest message read so far until the source's back-read is done; 'newer' reads what was posted after the newest message read, page by page until caught up. Once older reading is done, come back with 'newer' to monitor the source. The position is kept between calls."`
	Limit          Int64    `json:"limit,omitempty" jsonschema:"type=integer" jsonschema_description:"history only: messages per page (1-200, default 100)"`
	AllMessages    Bool     `json:"all_messages,omitempty" jsonschema:"type=boolean" jsonschema_description:"history only: true returns every message read; by default only the messages the judgement model (Jev) picks as worth a closer look are returned and the rest are counted in skipped_count"`
	PlatformUserID IDString `json:"platform_user_id,omitempty" jsonschema:"type=string" jsonschema_description:"user only: the sender id from a history page"`
	Message        string   `json:"message" jsonschema:"required,title=Sync source message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on what you are reading and why. Written in the engagement language declared by your system prompt."`
}

type GrowthEvidenceArg struct {
	SourceID    Int64    `json:"source_id" jsonschema:"required,type=integer" jsonschema_description:"The pool source_id the message or post was read from"`
	MessageID   IDString `json:"message_id,omitempty" jsonschema:"type=string" jsonschema_description:"Telegram: the message_id from the history page (required). Web: optional, the post URL or anchor; defaults to message_link."`
	MessageLink string   `json:"message_link,omitempty" jsonschema_description:"Link that opens the message or post"`
	MessageTime string   `json:"message_time,omitempty" jsonschema_description:"When it was posted, RFC 3339 (e.g. 2026-10-16T09:30:00Z), taken from the tool output or the page"`
	CapturedAt  string   `json:"captured_at,omitempty" jsonschema_description:"Web only: when you read the page, RFC 3339; defaults to now"`
	Excerpt     string   `json:"excerpt" jsonschema:"required" jsonschema_description:"The message text quoted verbatim in its original language; never translated or paraphrased"`
	MatchReason string   `json:"match_reason,omitempty" jsonschema_description:"Which signal this message shows, in one short phrase. Written in the engagement language declared by your system prompt."`
}

type GrowthLeadArg struct {
	Platform       String              `json:"platform,omitempty" jsonschema:"type=string,enum=telegram,enum=web" jsonschema_description:"Informational: the platform is taken from the evidence sources"`
	PlatformUserID IDString            `json:"platform_user_id" jsonschema:"required,type=string" jsonschema_description:"Telegram: the stable numeric sender id from the tool output. Web: the author's profile URL, or 'host:username' (e.g. forum.example.com:alice) when no profile link exists."`
	Username       string              `json:"username,omitempty" jsonschema_description:"Username exactly as the tool output or page shows it"`
	DisplayName    string              `json:"display_name,omitempty" jsonschema_description:"Display name exactly as shown"`
	Score          Int64               `json:"score,omitempty" jsonschema:"type=integer" jsonschema_description:"Your total signal score, needed only when no evidence message carries a score from growth_sync_source (web pages read with the browser). A scored message's score is used instead. At the qualified line of the business profile's Scoring line or above the lead counts toward the target; from its watchlist line it is kept for review; below it is not saved."`
	Tier           String              `json:"tier,omitempty" jsonschema:"type=string,enum=qualified,enum=watchlist" jsonschema_description:"Your tier; the stored tier always follows from the score"`
	MatchReason    string              `json:"match_reason" jsonschema:"required" jsonschema_description:"One or two sentences on why this person may become a customer, citing the signals of the evidence. Written in the engagement language declared by your system prompt."`
	Contacts       []GrowthContactArg  `json:"contacts,omitempty" jsonschema:"type=array" jsonschema_description:"Ways to reach this person beside the platform account, only of the types the business profile asks for and only as the person published them in the evidence or their public profile; never guessed"`
	Evidence       []GrowthEvidenceArg `json:"evidence" jsonschema:"required" jsonschema_description:"One or more messages or posts by this person that show the signals"`
}

type GrowthContactArg struct {
	Type  String `json:"type" jsonschema:"required,type=string" jsonschema_description:"phone, email, whatsapp, line, telegram (a number or id on that app), other, or another type exactly as the profile's Contacts to find names it"`
	Value string `json:"value" jsonschema:"required" jsonschema_description:"The contact exactly as published, e.g. '+66 81 234 5678' or '@ann'"`
}

type GrowthUpsertLeadsAction struct {
	Leads   []GrowthLeadArg `json:"leads" jsonschema:"required" jsonschema_description:"Batch of up to 20 leads. Every value must come from tool output or the page read; never invent fields."`
	Message string          `json:"message" jsonschema:"required,title=Save leads message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on the batch being saved. Written in the engagement language declared by your system prompt."`
}

type GrowthGetProgressAction struct {
	Target  Int64  `json:"target,omitempty" jsonschema:"type=integer" jsonschema_description:"Set the round's target when the customer named a number (default 50); omit to only read progress"`
	Since   string `json:"since,omitempty" jsonschema_description:"Outside a task: count only leads first found at or after this time, RFC 3339 with the user's offset (e.g. 2026-10-17T00:00:00+07:00 for today in Bangkok); a bare date counts from 00:00 UTC"`
	Message string `json:"message" jsonschema:"required,title=Progress message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on why you are checking progress. Written in the engagement language declared by your system prompt."`
}

type GrowthDeliverLeadsAction struct {
	AllowPartial Bool   `json:"allow_partial,omitempty" jsonschema:"type=boolean" jsonschema_description:"Deliver fewer than the target. Set it only when every source is exhausted or blocked; otherwise a short round delivers nothing and says how many more are needed."`
	HasContact   Bool   `json:"has_contact,omitempty" jsonschema:"type=boolean" jsonschema_description:"Deliver only leads with a contact beside their platform account; set it only when the user asked for that"`
	Message      string `json:"message" jsonschema:"required,title=Deliver leads message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on closing the round. Written in the engagement language declared by your system prompt."`
}

type GrowthGetProfileAction struct {
	ProfileID Int64  `json:"profile_id,omitempty" jsonschema:"type=integer" jsonschema_description:"A saved profile to read; omit for the profile this task uses"`
	Message   string `json:"message" jsonschema:"required,title=Get profile message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on why you read the profile. Written in the engagement language declared by your system prompt."`
}

type GrowthDraftProfileAction struct {
	Title     string         `json:"title" jsonschema:"required" jsonschema_description:"Short name of the profile in the user's language, e.g. the audience and the region"`
	ProfileID Int64          `json:"profile_id,omitempty" jsonschema:"type=integer" jsonschema_description:"The saved profile this draft changes, from growth_get_profile; omit to draft a new profile"`
	Profile   growth.Profile `json:"profile" jsonschema:"required" jsonschema_description:"The whole profile, not only the changed fields"`
	Message   string         `json:"message" jsonschema:"required,title=Draft profile message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on what the draft says or changes. Written in the engagement language declared by your system prompt."`
}

type GrowthSaveProfileAction struct {
	AsNew       Bool   `json:"as_new,omitempty" jsonschema:"type=boolean" jsonschema_description:"Save as a new profile even when the draft changes a saved one"`
	UseForTask  Bool   `json:"use_for_task,omitempty" jsonschema:"type=boolean" jsonschema_description:"Make this task find and judge leads by the saved profile from now on"`
	MakeDefault Bool   `json:"make_default,omitempty" jsonschema:"type=boolean" jsonschema_description:"Make it the profile new tasks start with; only when the user asked for that"`
	Message     string `json:"message" jsonschema:"required,title=Save profile message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on saving the profile. Written in the engagement language declared by your system prompt."`
}

type GrowthListLeadsAction struct {
	Scope      String `json:"scope,omitempty" jsonschema:"type=string,enum=flow,enum=task,enum=delivered,enum=all" jsonschema_description:"'flow' (default) leads seen in this flow; 'task' leads touched by the current task; 'delivered' leads already delivered from this flow; 'all' every lead of the user"`
	Since      string `json:"since,omitempty" jsonschema_description:"Only leads first found at or after this time, RFC 3339 with the user's offset (e.g. 2026-10-17T00:00:00+07:00); a bare date counts from 00:00 UTC"`
	Tier       String `json:"tier,omitempty" jsonschema:"type=string,enum=qualified,enum=watchlist,enum=all" jsonschema_description:"Filter by tier (default all)"`
	HasContact Bool   `json:"has_contact,omitempty" jsonschema:"type=boolean" jsonschema_description:"true lists only leads with a contact beside their platform account"`
	Limit      Int64  `json:"limit,omitempty" jsonschema:"type=integer" jsonschema_description:"Maximum leads (1-500, default 100)"`
	Message    string `json:"message" jsonschema:"required,title=List leads message" jsonschema_description:"Engagement-log entry — a 1-2 short sentence running commentary on why you are listing leads. Written in the engagement language declared by your system prompt."`
}
