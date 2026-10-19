package tools

import (
	"maps"
	"pentagi/pkg/database"
	"pentagi/pkg/growth"

	"github.com/invopop/jsonschema"
	"github.com/vxcontrol/langchaingo/llms"
)

const (
	FinalyToolName              = "done"
	AskUserToolName             = "ask"
	MaintenanceToolName         = "maintenance"
	MaintenanceResultToolName   = "maintenance_result"
	CoderToolName               = "coder"
	CodeResultToolName          = "code_result"
	PentesterToolName           = "pentester"
	HackResultToolName          = "hack_result"
	AdviceToolName              = "advice"
	MemoristToolName            = "memorist"
	MemoristResultToolName      = "memorist_result"
	BrowserToolName             = "browser"
	GoogleToolName              = "google"
	DuckDuckGoToolName          = "duckduckgo"
	TavilyToolName              = "tavily"
	FirecrawlToolName           = "firecrawl"
	TraversaalToolName          = "traversaal"
	PerplexityToolName          = "perplexity"
	SearxngToolName             = "searxng"
	SploitusToolName            = "sploitus"
	WebSearchToolName           = "web_search"
	SearchToolName              = "search"
	SearchResultToolName        = "search_result"
	EnricherResultToolName      = "enricher_result"
	SearchInMemoryToolName      = "search_in_memory"
	SearchGuideToolName         = "search_guide"
	StoreGuideToolName          = "store_guide"
	SearchAnswerToolName        = "search_answer"
	StoreAnswerToolName         = "store_answer"
	SearchCodeToolName          = "search_code"
	StoreCodeToolName           = "store_code"
	GraphitiSearchToolName      = "graphiti_search"
	ReportResultToolName        = "report_result"
	SubtaskListToolName         = "subtask_list"
	SubtaskPatchToolName        = "subtask_patch"
	TerminalToolName            = "terminal"
	FileToolName                = "file"
	GetFlowStatusToolName       = "get_flow_status"
	StopFlowToolName            = "stop_flow"
	SubmitFlowInputToolName     = "submit_flow_input"
	PatchFlowSubtasksToolName   = "patch_flow_subtasks"
	WaitFlowCompletionToolName  = "wait_flow_completion"
	GrowthSearchSourcesToolName = "growth_search_sources"
	GrowthAddSourcesToolName    = "growth_add_sources"
	GrowthJoinSourceToolName    = "growth_join_source"
	GrowthSyncSourceToolName    = "growth_sync_source"
	GrowthUpsertLeadsToolName   = "growth_upsert_leads"
	GrowthGetProgressToolName   = "growth_get_progress"
	GrowthDeliverLeadsToolName  = "growth_deliver_leads"
	GrowthListLeadsToolName     = "growth_list_leads"
	GrowthGetProfileToolName    = "growth_get_profile"
	GrowthDraftProfileToolName  = "growth_draft_profile"
	GrowthSaveProfileToolName   = "growth_save_profile"
	GrowthUpdateProfileToolName = "growth_update_profile"
)

type ToolType int

const (
	NoneToolType ToolType = iota
	EnvironmentToolType
	SearchNetworkToolType
	SearchVectorDbToolType
	AgentToolType
	StoreAgentResultToolType
	StoreVectorDbToolType
	BarrierToolType
)

func (t ToolType) String() string {
	switch t {
	case EnvironmentToolType:
		return "environment"
	case SearchNetworkToolType:
		return "search_network"
	case SearchVectorDbToolType:
		return "search_vector_db"
	case AgentToolType:
		return "agent"
	case StoreAgentResultToolType:
		return "store_agent_result"
	case StoreVectorDbToolType:
		return "store_vector_db"
	case BarrierToolType:
		return "barrier"
	default:
		return "none"
	}
}

// GetToolType returns the tool type for a given tool name
func GetToolType(name string) ToolType {
	if toolType, ok := toolsTypeMapping[name]; ok {
		return toolType
	}
	return NoneToolType
}

var toolsTypeMapping = map[string]ToolType{
	FinalyToolName:              BarrierToolType,
	AskUserToolName:             BarrierToolType,
	MaintenanceToolName:         AgentToolType,
	MaintenanceResultToolName:   StoreAgentResultToolType,
	CoderToolName:               AgentToolType,
	CodeResultToolName:          StoreAgentResultToolType,
	PentesterToolName:           AgentToolType,
	HackResultToolName:          StoreAgentResultToolType,
	AdviceToolName:              AgentToolType,
	MemoristToolName:            AgentToolType,
	MemoristResultToolName:      StoreAgentResultToolType,
	BrowserToolName:             SearchNetworkToolType,
	GoogleToolName:              SearchNetworkToolType,
	DuckDuckGoToolName:          SearchNetworkToolType,
	TavilyToolName:              SearchNetworkToolType,
	FirecrawlToolName:           SearchNetworkToolType,
	TraversaalToolName:          SearchNetworkToolType,
	PerplexityToolName:          SearchNetworkToolType,
	SearxngToolName:             SearchNetworkToolType,
	SploitusToolName:            SearchNetworkToolType,
	WebSearchToolName:           SearchNetworkToolType,
	SearchToolName:              AgentToolType,
	SearchResultToolName:        StoreAgentResultToolType,
	EnricherResultToolName:      StoreAgentResultToolType,
	SearchInMemoryToolName:      SearchVectorDbToolType,
	SearchGuideToolName:         SearchVectorDbToolType,
	StoreGuideToolName:          StoreVectorDbToolType,
	SearchAnswerToolName:        SearchVectorDbToolType,
	StoreAnswerToolName:         StoreVectorDbToolType,
	SearchCodeToolName:          SearchVectorDbToolType,
	StoreCodeToolName:           StoreVectorDbToolType,
	GraphitiSearchToolName:      SearchVectorDbToolType,
	ReportResultToolName:        StoreAgentResultToolType,
	SubtaskListToolName:         StoreAgentResultToolType,
	SubtaskPatchToolName:        StoreAgentResultToolType,
	TerminalToolName:            EnvironmentToolType,
	FileToolName:                EnvironmentToolType,
	GetFlowStatusToolName:       EnvironmentToolType,
	StopFlowToolName:            EnvironmentToolType,
	SubmitFlowInputToolName:     EnvironmentToolType,
	PatchFlowSubtasksToolName:   EnvironmentToolType,
	WaitFlowCompletionToolName:  EnvironmentToolType,
	GrowthSearchSourcesToolName: SearchNetworkToolType,
	GrowthAddSourcesToolName:    EnvironmentToolType,
	GrowthJoinSourceToolName:    EnvironmentToolType,
	GrowthSyncSourceToolName:    SearchNetworkToolType,
	GrowthUpsertLeadsToolName:   EnvironmentToolType,
	GrowthGetProgressToolName:   EnvironmentToolType,
	GrowthDeliverLeadsToolName:  EnvironmentToolType,
	GrowthListLeadsToolName:     EnvironmentToolType,
	GrowthGetProfileToolName:    EnvironmentToolType,
	GrowthDraftProfileToolName:  EnvironmentToolType,
	GrowthSaveProfileToolName:   EnvironmentToolType,
	GrowthUpdateProfileToolName: EnvironmentToolType,
}

var reflector = &jsonschema.Reflector{
	DoNotReference: true,
	ExpandedStruct: true,
}

// A rendered browser page routinely exceeds the result size limit; a tool off this list hands it over whole.
var allowedSummarizingToolsResult = []string{
	TerminalToolName,
	BrowserToolName,
}

// Results later subtasks recall from memory; losing the search agent's findings degrades them silently.
var allowedStoringInMemoryTools = []string{
	TerminalToolName,
	FileToolName,
	SearchToolName,
	GoogleToolName,
	DuckDuckGoToolName,
	TavilyToolName,
	FirecrawlToolName,
	TraversaalToolName,
	PerplexityToolName,
	SearxngToolName,
	SploitusToolName,
	WebSearchToolName,
	MaintenanceToolName,
	CoderToolName,
	PentesterToolName,
	AdviceToolName,
}

var registryDefinitions = map[string]llms.FunctionDefinition{
	TerminalToolName: {
		Name: TerminalToolName,
		Description: "Calls a terminal command in blocking mode. " +
			"Use timeout=0 or a negative value to apply the configured server default timeout. " +
			"Explicit positive values are accepted up to 10800 seconds (3 hours); values outside this range are replaced by the server default. " +
			"Only one command can be executed at a time",
		Parameters: reflector.Reflect(&TerminalAction{}),
	},
	FileToolName: {
		Name: FileToolName,
		Description: "Reads, writes, or edits local files (see 'action'). " +
			"Prefer edit_file for targeted changes to an existing file; use write_file only for a new file or a full rewrite.",
		Parameters: reflector.Reflect(&FileAction{}),
	},
	ReportResultToolName: {
		Name:        ReportResultToolName,
		Description: "Send the report result to the user with execution status and description",
		Parameters:  reflector.Reflect(&TaskResult{}),
	},
	SubtaskListToolName: {
		Name:        SubtaskListToolName,
		Description: "Send new generated subtask list to the user",
		Parameters:  reflector.Reflect(&SubtaskList{}),
	},
	SubtaskPatchToolName: {
		Name: SubtaskPatchToolName,
		Description: "Submit delta operations to modify the current subtask list instead of regenerating all subtasks. " +
			"Supports add (create new subtask at position), remove (delete by ID), modify (update title/description), " +
			"and reorder (move to different position) operations. Use empty operations array if no changes needed.",
		Parameters: reflector.Reflect(&SubtaskPatch{}),
	},
	SearchToolName: {
		Name: SearchToolName,
		Description: "Search in a different search engines in the internet and long-term memory " +
			"by your complex question to the researcher team member, also you can add some instructions to get result " +
			"in a specific format or structure or content type like " +
			"community and group links, forum threads, audience research, manuals, guides, code samples, libraries, etc.",
		Parameters: reflector.Reflect(&ComplexSearch{}),
	},
	SearchResultToolName: {
		Name:        SearchResultToolName,
		Description: "Send the complex search result as a answer for the user question to the user",
		Parameters:  reflector.Reflect(&SearchResult{}),
	},
	BrowserToolName: {
		Name:        BrowserToolName,
		Description: "Opens a browser to look for additional information from the web site",
		Parameters:  reflector.Reflect(&Browser{}),
	},
	GoogleToolName: {
		Name: GoogleToolName,
		Description: "Search in the google search engine, it's a fast query and the shortest content " +
			"to check some information or collect public links by short query",
		Parameters: reflector.Reflect(&SearchAction{}),
	},
	DuckDuckGoToolName: {
		Name: DuckDuckGoToolName,
		Description: "Search in the duckduckgo search engine, it's a anonymous query and returns a small content " +
			"to check some information from different sources or collect public links by short query",
		Parameters: reflector.Reflect(&SearchAction{}),
	},
	TavilyToolName: {
		Name: TavilyToolName,
		Description: "Search in the tavily search engine, it's a more complex query and more detailed content " +
			"with answer by query and detailed information from the web sites",
		Parameters: reflector.Reflect(&SearchAction{}),
	},
	FirecrawlToolName: {
		Name: FirecrawlToolName,
		Description: "Search in the firecrawl search engine, it combines web search with page scraping to return " +
			"the main-content markdown for each result, ideal for deep research on complex technical topics " +
			"and reading documentation directly from the discovered web sites",
		Parameters: reflector.Reflect(&SearchAction{}),
	},
	TraversaalToolName: {
		Name: TraversaalToolName,
		Description: "Search in the traversaal search engine, presents you answer and web-links " +
			"by your query according to relevant information from the web sites",
		Parameters: reflector.Reflect(&SearchAction{}),
	},
	PerplexityToolName: {
		Name: PerplexityToolName,
		Description: "Search in the perplexity search engine, it's a fully complex query and detailed research report " +
			"with answer by query and detailed information from the web sites and other sources augmented by the LLM",
		Parameters: reflector.Reflect(&SearchAction{}),
	},
	SearxngToolName: {
		Name: SearxngToolName,
		Description: "Search in the searxng meta search engine, it's a privacy-focused search engine " +
			"that aggregates results from multiple search engines with customizable categories, " +
			"language settings, and safety filters",
		Parameters: reflector.Reflect(&SearchAction{}),
	},
	SploitusToolName: {
		Name: SploitusToolName,
		Description: "Search the Sploitus exploit aggregator (https://sploitus.com) for public exploits, " +
			"proof-of-concept code, and offensive security tools. Sploitus indexes ExploitDB, Packet Storm, " +
			"GitHub Security Advisories, and many other sources. Use this tool to find exploit code and PoCs " +
			"for specific software, services, CVEs, or vulnerability classes (e.g. 'ssh', 'apache log4j', " +
			"'CVE-2021-44228'). Returns exploit URLs, CVSS scores, CVE references, and publication dates.",
		Parameters: reflector.Reflect(&SploitusAction{}),
	},
	WebSearchToolName: {
		Name: WebSearchToolName,
		Description: "Search the web through a unified engine. Provide a `query` and a `mode`: " +
			"`links` for a quick list of source links with snippets, `answer` for a synthesized answer over " +
			"live sources (default), or `research` for deep multi-source analysis with reasoning. The tool selects the best available search provider for " +
			"that mode, retries transient failures, and automatically falls back to alternative providers, so you " +
			"do NOT choose or name a specific engine. Queries must be short, keyword-focused, and in English.",
		Parameters: reflector.Reflect(&WebSearchAction{}),
	},
	EnricherResultToolName: {
		Name:        EnricherResultToolName,
		Description: "Send the enriched user's question with additional information to the user",
		Parameters:  reflector.Reflect(&EnricherResult{}),
	},
	SearchInMemoryToolName: {
		Name: SearchInMemoryToolName,
		Description: "Search in the vector database (long-term memory) for relevant information by providing one or more semantically rich, " +
			"context-aware natural language queries (1 to 5 queries). Formulate each query with sufficient context, intent, and detailed descriptions " +
			"to enhance semantic matching and retrieval accuracy. Multiple queries allow exploring different semantic angles and improve recall. " +
			"Results from all queries are merged, deduplicated, and ranked by relevance score. This function is ideal when you need to retrieve specific information " +
			"to assist in generating accurate and informative responses. If Task ID or Subtask ID are known, " +
			"they can be used as strict filters to further refine the search results and improve relevancy.",
		Parameters: reflector.Reflect(&SearchInMemoryAction{}),
	},
	SearchGuideToolName: {
		Name: SearchGuideToolName,
		Description: "Search in the vector database for relevant guides by providing one or more semantically rich, context-aware natural language queries (1 to 5 queries). " +
			"Formulate each query with sufficient context, intent, and detailed descriptions of the guides you need to enhance semantic matching and " +
			"retrieval accuracy. Multiple queries allow exploring different aspects of the guide topic and improve search coverage. " +
			"Specify the type of guide required to further refine the search. Results from all queries are merged, deduplicated, and ranked by relevance score. " +
			"This function is ideal when you need to retrieve specific guides to assist in accomplishing tasks or solving issues.",
		Parameters: reflector.Reflect(&SearchGuideAction{}),
	},
	StoreGuideToolName: {
		Name: StoreGuideToolName,
		Description: "Store the guide to the vector database for future use. " +
			"Anonymize all sensitive data (IPs, domains, credentials, paths) using descriptive placeholders",
		Parameters: reflector.Reflect(&StoreGuideAction{}),
	},
	SearchAnswerToolName: {
		Name: SearchAnswerToolName,
		Description: "Search in the vector database for relevant answers by providing one or more semantically rich, context-aware natural language queries (1 to 5 queries). " +
			"Formulate each query with sufficient context, intent, and detailed descriptions of what you want to find and why you need it " +
			"to enhance semantic matching and retrieval accuracy. Multiple queries allow exploring different formulations and improve search coverage. " +
			"Specify the type of answer required to further refine the search. Results from all queries are merged, deduplicated, and ranked by relevance score. " +
			"This function is ideal when you need to retrieve specific answers to assist in tasks, solve issues, or answer questions.",
		Parameters: reflector.Reflect(&SearchAnswerAction{}),
	},
	StoreAnswerToolName: {
		Name: StoreAnswerToolName,
		Description: "Store the question answer to the vector database for future use. " +
			"Anonymize all sensitive data (IPs, domains, credentials) using descriptive placeholders",
		Parameters: reflector.Reflect(&StoreAnswerAction{}),
	},
	SearchCodeToolName: {
		Name: SearchCodeToolName,
		Description: "Search in the vector database for relevant code samples by providing one or more semantically rich, context-aware natural language queries (1 to 5 queries). " +
			"Formulate each query with sufficient context, intent, and detailed descriptions of what you want to achieve with the code and what should be included, " +
			"to enhance semantic matching and retrieval accuracy. Multiple queries allow exploring different code patterns and use cases. " +
			"Specify the programming language to further refine the search. Results from all queries are merged, deduplicated, and ranked by relevance score. " +
			"This function is ideal when you need to retrieve specific code examples to assist in development tasks or solve programming issues.",
		Parameters: reflector.Reflect(&SearchCodeAction{}),
	},
	StoreCodeToolName: {
		Name: StoreCodeToolName,
		Description: "Store the code sample to the vector database for future use. It's should be a sample like a one source code file for some question. " +
			"Anonymize all sensitive data (IPs, domains, credentials, API keys) using descriptive placeholders",
		Parameters: reflector.Reflect(&StoreCodeAction{}),
	},
	GraphitiSearchToolName: {
		Name: GraphitiSearchToolName,
		Description: "Search the Graphiti temporal knowledge graph for historical context of this engagement, " +
			"including previous agent responses, tool execution records, discovered entities, and their relationships. " +
			"Supports 7 search types: temporal_window (time-bounded search), entity_relationships (graph traversal from an entity), " +
			"diverse_results (anti-redundancy search), episode_context (full agent reasoning and tool outputs), " +
			"successful_tools (proven techniques), recent_context (latest findings), and entity_by_label (type-specific entity search). " +
			"Use this to avoid repeating failed approaches, reuse successful techniques, understand entity relationships, " +
			"and build on previous findings within the same engagement.",
		Parameters: reflector.Reflect(&GraphitiSearchAction{}),
	},
	MemoristToolName: {
		Name:        MemoristToolName,
		Description: "Call to Archivist team member who remember all the information about the past work and made tasks and can answer your question about it",
		Parameters:  reflector.Reflect(&MemoristAction{}),
	},
	MemoristResultToolName: {
		Name:        MemoristResultToolName,
		Description: "Send the search in long-term memory result as a answer for the user question to the user",
		Parameters:  reflector.Reflect(&MemoristResult{}),
	},
	MaintenanceToolName: {
		Name:        MaintenanceToolName,
		Description: "Call to DevOps team member to maintain local environment and tools inside the docker container",
		Parameters:  reflector.Reflect(&MaintenanceAction{}),
	},
	MaintenanceResultToolName: {
		Name:        MaintenanceResultToolName,
		Description: "Send the maintenance result to the user with task status and fully detailed report about using the result",
		Parameters:  reflector.Reflect(&TaskResult{}),
	},
	CoderToolName: {
		Name:        CoderToolName,
		Description: "Call to developer team member to write a code for the specific task",
		Parameters:  reflector.Reflect(&CoderAction{}),
	},
	CodeResultToolName: {
		Name:        CodeResultToolName,
		Description: "Send the code result to the user with execution status and fully detailed report about using the result",
		Parameters:  reflector.Reflect(&CodeResult{}),
	},
	PentesterToolName: {
		Name:        PentesterToolName,
		Description: "Call the lead research specialist to read sources, qualify the people posting there, and save qualified leads with evidence",
		Parameters:  reflector.Reflect(&PentesterAction{}),
	},
	HackResultToolName: {
		Name:        HackResultToolName,
		Description: "Send the lead research result to the user with detailed report",
		Parameters:  reflector.Reflect(&HackResult{}),
	},
	AdviceToolName: {
		Name:        AdviceToolName,
		Description: "Get more complex answer from the mentor about some issue or difficult situation",
		Parameters:  reflector.Reflect(&AskAdvice{}),
	},
	AskUserToolName: {
		Name:        AskUserToolName,
		Description: "If you need to ask user for input, use this tool",
		Parameters:  reflector.Reflect(&AskUser{}),
	},
	FinalyToolName: {
		Name:        FinalyToolName,
		Description: "If you need to finish the task with success or failure, use this tool",
		Parameters:  reflector.Reflect(&Done{}),
	},
	GetFlowStatusToolName: {
		Name: GetFlowStatusToolName,
		Description: "Return structured information about the automation flow. " +
			"Choose the detail level based on what you need to know:\n" +
			"  'summary'  — flow health snapshot: status, task counts, subtask counts, active task/subtask IDs. Best first call.\n" +
			"  'tasks'    — all tasks with ID, status, title. Add verbose=true for inputs and results.\n" +
			"  'subtasks' — all subtasks, optionally filtered by task_id. Add verbose=true for descriptions and results.\n" +
			"  'running'  — full Task→Subtask execution chain: task input, subtask description, partial result, and recent agent messages (10 default, 50 with verbose=true). Use when you need to understand what is happening right now.\n" +
			"  'planned'  — subtasks with status 'created' (not yet started), optionally filtered by task_id. Add verbose=true for full descriptions.",
		Parameters: reflector.Reflect(&GetFlowStatusAction{}),
	},
	StopFlowToolName: {
		Name: StopFlowToolName,
		Description: "Cancel the currently executing automation task. " +
			"If a task is actively running its execution context is cancelled; the flow transitions to 'waiting' state and can accept new input or subtask patches. " +
			"If no task is running the tool returns an informational message and takes no further action. " +
			"After calling this tool, verify the flow reached 'waiting' state before making further changes.",
		Parameters: reflector.Reflect(&StopFlowAction{}),
	},
	SubmitFlowInputToolName: {
		Name: SubmitFlowInputToolName,
		Description: "Deliver text to the automation flow. The behaviour depends on the current flow state:\n" +
			"  Subtask at 'ask' checkpoint — the text is delivered as the user's answer and the subtask resumes immediately.\n" +
			"  Flow waiting with no active subtask — the text becomes the goal for a new task; the generator and refiner agents decompose it into a subtask list. Write a complete, detailed description with all context the automation needs.\n" +
			"Returns an error if a task is currently running — cancel the running task first.",
		Parameters: reflector.Reflect(&SubmitFlowInputAction{}),
	},
	PatchFlowSubtasksToolName: {
		Name: PatchFlowSubtasksToolName,
		Description: "Replace the planned (not-yet-started) subtask list for a specific task using delta operations. " +
			"Supported operations: add (insert a new subtask at a position), remove (delete a planned subtask by ID), modify (update title or description), reorder (move to a different position). " +
			"The currently executing or waiting subtask can also be targeted by including its ID in an operation; doing so resets it to 'created' status. " +
			"After patching, submit new input to trigger execution of the updated plan. " +
			"Returns an error if a task is currently running — cancel it first.",
		Parameters: reflector.Reflect(&PatchFlowSubtasksAction{}),
	},
	GrowthSearchSourcesToolName: {
		Name: GrowthSearchSourcesToolName,
		Description: "Find lead sources and add them to the customer's source pool: public Telegram groups and channels " +
			"by keyword (mode=search), links found elsewhere including forum threads and web pages (mode=resolve), " +
			"or Telegram channels similar to a pool source (mode=recommend). Returns each source with its source_id, " +
			"membership and how much of it has been read.",
		Parameters: reflector.Reflect(&GrowthSearchSourcesAction{}),
	},
	GrowthAddSourcesToolName: {
		Name: GrowthAddSourcesToolName,
		Description: "Add sources the customer supplied by hand (Telegram links, @usernames, invite links, forum or web page URLs) " +
			"to the source pool. They are deduplicated against sources already found and processed like them.",
		Parameters: reflector.Reflect(&GrowthAddSourcesAction{}),
	},
	GrowthJoinSourceToolName: {
		Name: GrowthJoinSourceToolName,
		Description: "Join pool sources with the customer's Telegram account and record the outcome per source: joined, " +
			"pending (join request awaits approval) or not_joinable. Already joined sources are skipped. " +
			"Many public groups can be read without joining; join when reading fails or the group is private.",
		Parameters: reflector.Reflect(&GrowthJoinSourceAction{}),
	},
	GrowthSyncSourceToolName: {
		Name: GrowthSyncSourceToolName,
		Description: "Read pool sources' public messages (action=history), one source or a batch of source_ids. 'older' reads " +
			"backwards one page per call until the source's back-read is done; 'newer' then reads what was posted since, " +
			"page by page until caught up, which is how a source is monitored. The read position is kept between calls. " +
			"Only the messages the judgement model (Jev) picks as worth a closer look are returned unless all_messages is true; " +
			"each has its sender id, username, time, link, its opening 400 characters as text, and its score, tier and signals weighted by the business profile. " +
			"A result holds as many messages as fit its size limit: when more wait, 'pending' says how many and the read position " +
			"stays before them, so the next call with the same direction continues there and nothing is skipped. " +
			"action=message returns one message's full text for an evidence excerpt and does not move the read position. " +
			"action=user returns a sender's public profile. Web sources are read with the browser tool instead.",
		Parameters: reflector.Reflect(&GrowthSyncSourceAction{}),
	},
	GrowthUpsertLeadsToolName: {
		Name: GrowthUpsertLeadsToolName,
		Description: "Save scored leads with their evidence and contacts. A lead whose evidence growth_sync_source scored takes that score; " +
			"the excerpts of any other lead are scored by the judgement model (Jev), whose score replaces the one given. " +
			"A person is stored once per customer however often they are saved: a repeat adds evidence and contacts and may raise the score. " +
			"Returns per lead NEW, EXISTING, UPGRADED (watchlist to qualified) or REJECTED with the reason, and the progress line. " +
			"Only qualified leads (at the qualified line of the business profile's Jev score section) count toward the target.",
		Parameters: reflector.Reflect(&GrowthUpsertLeadsAction{}),
	},
	GrowthGetProgressToolName: {
		Name: GrowthGetProgressToolName,
		Description: "Return the current round's progress from the database: qualified leads collected this round " +
			"(new_leads_this_task), the target, watchlist leads, and the state of the source pool, followed by the line " +
			"'PROGRESS: new_leads_this_task=<n> target=<t>'. Pass target to set the round's goal when the customer named one.",
		Parameters: reflector.Reflect(&GrowthGetProgressAction{}),
	},
	GrowthDeliverLeadsToolName: {
		Name: GrowthDeliverLeadsToolName,
		Description: "Close the round: mark the best qualified leads up to the target as delivered and return the list " +
			"with evidence. Delivered people are never delivered again in a later round. If the target is not reached it " +
			"delivers nothing and says how many more are needed, unless allow_partial is set.",
		Parameters: reflector.Reflect(&GrowthDeliverLeadsAction{}),
	},
	GrowthListLeadsToolName: {
		Name: GrowthListLeadsToolName,
		Description: "List saved leads with their evidence, read-only, for reporting. A result holds as many leads as fit " +
			"its size limit; when more were listed, its note gives the offset to continue from.",
		Parameters: reflector.Reflect(&GrowthListLeadsAction{}),
	},
	GrowthGetProfileToolName: {
		Name: GrowthGetProfileToolName,
		Description: "Read a business profile (who to find, who is unwanted, how messages are scored): the one this task " +
			"uses, or a saved one by profile_id. Also lists the user's saved profiles and the exclusions the system always applies.",
		Parameters: reflector.Reflect(&GrowthGetProfileAction{}),
	},
	GrowthDraftProfileToolName: {
		Name: GrowthDraftProfileToolName,
		Description: "Show the user a profile draft as a card on the task page, replacing the previous draft; nothing is saved. " +
			"Call it when the user asks for a new profile or a change to one, with the whole profile. " +
			"Returns the draft as it will be saved, or why it cannot be.\n" + growth.ProfileGuide,
		Parameters: reflector.Reflect(&GrowthDraftProfileAction{}),
	},
	GrowthSaveProfileToolName: {
		Name: GrowthSaveProfileToolName,
		Description: "Save the current draft once the user confirmed it: over the profile it changes, or as a new one. " +
			"use_for_task makes this task use it from its next step on. Returns the saved profile with its profile_id.",
		Parameters: reflector.Reflect(&GrowthSaveProfileAction{}),
	},
	GrowthUpdateProfileToolName: {
		Name: GrowthUpdateProfileToolName,
		Description: "Change one saved profile immediately: one of the three prompts (need, exclude, scoring) or an added prompt. " +
			"Use it when the user asks to change an existing profile, including \"add this to the exclusions\". " +
			"action=set replaces the text, action=append adds a line, action=add_section adds a prompt (section_title and text), " +
			"action=remove_section deletes one by section_id or section_title. set with section_id and new_title renames an added prompt; " +
			"set with section_id and text replaces its body. The previous text is kept so the user can restore it on the profile page.\n" +
			growth.ProfileGuide,
		Parameters: reflector.Reflect(&GrowthUpdateProfileAction{}),
	},
	WaitFlowCompletionToolName: {
		Name: WaitFlowCompletionToolName,
		Description: "Block until the currently running automation task completes (or the timeout expires). " +
			"Use this when you need to wait for the automation to finish before inspecting its results or taking further action. " +
			"Returns immediately with an informational message if no tasks exist or if no task is currently running — " +
			"use " + GetFlowStatusToolName + " first to confirm the flow state before calling this tool. " +
			"On return, always call " + GetFlowStatusToolName + " with detail='summary' to inspect the final status.",
		Parameters: reflector.Reflect(&WaitFlowCompletionAction{}),
	},
}

func getMessageType(name string) database.MsglogType {
	switch name {
	case TerminalToolName:
		return database.MsglogTypeTerminal
	case FileToolName:
		return database.MsglogTypeFile
	case BrowserToolName:
		return database.MsglogTypeBrowser
	case MemoristToolName, SearchToolName, GoogleToolName, DuckDuckGoToolName, TavilyToolName, FirecrawlToolName,
		TraversaalToolName, PerplexityToolName, SearxngToolName, SploitusToolName, WebSearchToolName,
		SearchGuideToolName, SearchAnswerToolName, SearchCodeToolName, SearchInMemoryToolName, GraphitiSearchToolName,
		GrowthSearchSourcesToolName, GrowthSyncSourceToolName:
		return database.MsglogTypeSearch
	case AdviceToolName:
		return database.MsglogTypeAdvice
	case AskUserToolName:
		return database.MsglogTypeAsk
	case FinalyToolName:
		return database.MsglogTypeDone
	default:
		return database.MsglogTypeThoughts
	}
}

func getMessageResultFormat(name string) database.MsglogResultFormat {
	switch name {
	case TerminalToolName:
		return database.MsglogResultFormatTerminal
	case FileToolName, BrowserToolName,
		GrowthSearchSourcesToolName, GrowthAddSourcesToolName, GrowthJoinSourceToolName, GrowthSyncSourceToolName,
		GrowthUpsertLeadsToolName, GrowthGetProgressToolName, GrowthDeliverLeadsToolName, GrowthListLeadsToolName,
		GrowthGetProfileToolName, GrowthDraftProfileToolName, GrowthSaveProfileToolName, GrowthUpdateProfileToolName:
		// The growth tools answer in JSON, which the Markdown view runs together.
		return database.MsglogResultFormatPlain
	default:
		return database.MsglogResultFormatMarkdown
	}
}

// GetRegistryDefinitions returns tool definitions from the tools package
func GetRegistryDefinitions() map[string]llms.FunctionDefinition {
	registry := make(map[string]llms.FunctionDefinition, len(registryDefinitions))
	maps.Copy(registry, registryDefinitions)
	return registry
}

// GetToolTypeMapping returns a mapping from tool names to tool types
func GetToolTypeMapping() map[string]ToolType {
	mapping := make(map[string]ToolType, len(toolsTypeMapping))
	maps.Copy(mapping, toolsTypeMapping)
	return mapping
}

// GetToolsByType returns a mapping from tool types to a list of tool names
func GetToolsByType() map[ToolType][]string {
	result := make(map[ToolType][]string)

	for toolName, toolType := range toolsTypeMapping {
		result[toolType] = append(result[toolType], toolName)
	}

	return result
}
