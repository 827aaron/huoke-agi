# Language model providers

KeLai AGI reads the language model settings below from `.env`; the language model card on the settings page covers the same providers for a single key. Each section lists the bundled models and which agent role each one serves by default. Role names in backticks are the internal agent ids: `pentester` is the prospect researcher and `searcher` the source scout.

### Custom LLM Provider Configuration

When using custom LLM providers with the `LLM_SERVER_*` variables, you can fine-tune the reasoning format used in requests.

> [!TIP]
> For production-grade local deployments, consider using **vLLM** with **Qwen3.5-27B-FP8** for optimal performance. See our [comprehensive deployment guide](../examples/guides/vllm-qwen35-27b-fp8.md) which includes hardware requirements, configuration templates ([thinking mode](../examples/configs/vllm-qwen3.5-27b-fp8.provider.yml) and [non-thinking mode](../examples/configs/vllm-qwen3.5-27b-fp8-no-think.provider.yml)), and performance benchmarks showing 13K TPS prompt processing on 4× RTX 5090 GPUs.

| Variable                        | Default      | Description                                                                                                        |
| ------------------------------- | ------------ | ------------------------------------------------------------------------------------------------------------------ |
| `LLM_SERVER_URL`                |              | Base URL for the custom LLM API endpoint                                                                           |
| `LLM_SERVER_KEY`                |              | API key for the custom LLM provider                                                                                |
| `LLM_SERVER_MODEL`              |              | Default model to use (can be overridden in provider config)                                                        |
| `LLM_SERVER_CONFIG_PATH`        |              | Path to the YAML configuration file for agent-specific models                                                      |
| `LLM_SERVER_PROVIDER`           |              | Provider name prefix for model names (e.g., `openrouter`, `deepseek` for LiteLLM proxy)                            |
| `LLM_SERVER_PRESERVE_REASONING` | `false`      | Preserve reasoning content in multi-turn conversations (required by some providers)                                |
| `LLM_SERVER_API_TYPE`           |              | `azure` or `azure_ad` for an Azure OpenAI deployment, empty for a plain endpoint; see [Using Azure OpenAI](#using-azure-openai) |
| `LLM_SERVER_API_VERSION`        | `2024-10-21` | The `api-version` Azure requires; ignored by a plain endpoint                                                      |

The `LLM_SERVER_PROVIDER` setting is particularly useful when using **LiteLLM proxy**, which adds a provider prefix to model names. For example, when connecting to Moonshot API through LiteLLM, models like `kimi-2.5` become `moonshot/kimi-2.5`. By setting `LLM_SERVER_PROVIDER=moonshot`, you can use the same provider configuration file for both direct API access and LiteLLM proxy access without modifications.

The `LLM_SERVER_PRESERVE_REASONING` setting controls whether reasoning content is preserved in multi-turn conversations:
- `false` (default): Reasoning content is not preserved in conversation history
- `true`: Reasoning content is preserved and sent in subsequent API calls

This setting is required by some LLM providers (e.g., Moonshot) that return errors like "thinking is enabled but reasoning_content is missing in assistant tool call message" when reasoning content is not included in multi-turn conversations. Enable this setting if your provider requires reasoning content to be preserved.

#### Using Azure OpenAI

The custom provider calls Azure OpenAI deployments directly:

```bash
LLM_SERVER_URL=https://<resource>.openai.azure.com  # the Endpoint from the resource's Keys and Endpoint page, without an /openai path
LLM_SERVER_KEY=your_azure_openai_api_key            # KEY 1 or KEY 2 from the same page
LLM_SERVER_API_TYPE=azure                           # the api-key header and deployment URLs
LLM_SERVER_API_VERSION=2024-10-21                   # the default; a newer version works as well
LLM_SERVER_MODEL=                                   # Leave empty, models are specified in the config
LLM_SERVER_CONFIG_PATH=/opt/pentagi/conf/azure-openai.provider.yml
```

Azure addresses a **deployment**, not a model: KeLai AGI calls `<LLM_SERVER_URL>/openai/deployments/<model>/chat/completions?api-version=<version>`, so every `model:` in the provider config must be the name of a deployment in your resource, and `LLM_SERVER_PROVIDER` must stay empty, because its prefix would become part of the deployment name. The bundled [`azure-openai.provider.yml`](../examples/configs/azure-openai.provider.yml) expects deployments named after their models: `gpt-4.1`, `gpt-4.1-mini` and `o4-mini`. If yours are named differently, copy the file, change the `model:` values, and mount your copy:

```bash
PENTAGI_LLM_SERVER_CONFIG_PATH=/path/on/host/my-azure.provider.yml  # mounted at /opt/pentagi/conf/custom.provider.yml
LLM_SERVER_CONFIG_PATH=/opt/pentagi/conf/custom.provider.yml
```

After `docker compose up -d`, check the deployments before running a flow. Without `-config`, `ctester` tests the file `LLM_SERVER_CONFIG_PATH` names, the bundled one or your copy:

```bash
docker exec -it pentagi /opt/pentagi/bin/ctester
```

- KeLai AGI does not read a model catalogue from Azure, so it knows no context window for a deployment and does not compact agent chains to fit one.
- `LLM_SERVER_API_TYPE=azure_ad` sends `LLM_SERVER_KEY` as a Microsoft Entra ID bearer token instead of an API key. KeLai AGI does not refresh it, and Entra tokens expire after about an hour, so use an API key for anything longer.
- Azure's OpenAI-compatible [v1 API](https://learn.microsoft.com/en-us/azure/foundry/openai/api-version-lifecycle) works as a plain endpoint too: set `LLM_SERVER_URL=https://<resource>.openai.azure.com/openai/v1` and leave `LLM_SERVER_API_TYPE` empty; `model:` is still the deployment name.
- The installer's custom provider form has **API Type** and **API Version** fields for the same two settings.
- Embeddings are configured separately through `EMBEDDING_*` and have no Azure mode; `LLM_SERVER_API_TYPE` does not apply to them.

#### Troubleshooting: tool-call (function-call) parser errors

KeLai AGI drives its agents with tool calls (also called function calls), so any custom OpenAI-compatible backend configured through `LLM_SERVER_*` must return valid tool-call JSON in the format the OpenAI Chat Completions API defines. When the backend emits malformed, truncated, or non-conforming tool-call arguments, the agent chain cannot continue.

Self-hosted engines such as llama.cpp, SGLang, and vLLM usually require a specific tool-call parser and a matching chat template to produce correct tool-call output. If the parser is missing or mismatched for the model you are serving, tool-call arguments can come back corrupted. Compatibility therefore depends on the backend's tool-call/function-call behavior and configuration, not on KeLai AGI alone; not every llama.cpp or SGLang setup produces valid tool calls out of the box.

Typical symptoms:

- Backend or proxy errors such as `Failed to parse tool call arguments as JSON` (often surfaced through a LiteLLM proxy as an HTTP 500), or other unexpected 5xx/4xx responses from the LLM endpoint.
- A flow that runs for a few steps and then stops responding to new input in the UI.
- Repeated or looping tool calls that never converge.
- A flow that fails right at the start with `failed to select primary docker image via llm call`, because the first action in a flow is an LLM tool call to choose the container image; a backend that cannot return a valid tool call fails at this step too.

How to investigate:

1. Check both sides of the connection: the KeLai AGI logs (`docker compose logs -f pentagi`) and the inference backend or proxy logs (llama.cpp, SGLang, vLLM, or LiteLLM). The backend log usually shows the same parse error when it produced the malformed tool call.
2. Validate the provider before running a full flow with the `ctester` utility, which exercises tool-calling agent types directly. Run it with `docker exec -it pentagi /opt/pentagi/bin/ctester`.
3. Confirm the backend's tool-call parser and chat template are the ones recommended for the model you are serving, and that the model itself supports tool calling.
4. Update KeLai AGI to the latest build. Recent versions sanitize malformed function-call arguments returned by the model so a single bad response no longer stalls the whole flow; older builds forwarded the corrupted arguments and could get stuck.

### Ollama Provider Configuration

KeLai AGI supports Ollama for both local LLM inference (zero-cost, enhanced privacy) and Ollama Cloud (managed service with free tier).

#### Configuration Variables

| Variable                            | Default     | Description                               |
| ----------------------------------- | ----------- | ----------------------------------------- |
| `OLLAMA_SERVER_URL`                 |             | URL of your Ollama server or Ollama Cloud |
| `OLLAMA_SERVER_API_KEY`             |             | API key for Ollama Cloud authentication   |
| `OLLAMA_SERVER_MODEL`               |             | Default model for inference               |
| `OLLAMA_SERVER_CONFIG_PATH`         |             | Path to custom agent configuration file   |
| `OLLAMA_SERVER_PULL_MODELS_TIMEOUT` | `600`       | Timeout for model downloads (seconds)     |
| `OLLAMA_SERVER_PULL_MODELS_ENABLED` | `false`     | Auto-download models on startup           |
| `OLLAMA_SERVER_LOAD_MODELS_ENABLED` | `false`     | Query server for available models         |

#### Ollama Cloud Configuration

Ollama Cloud provides managed inference with a free tier and paid plans. Paid usage is billed from included or purchased credits at each model's published per-token rate; off-peak discounts apply to selected DeepSeek models.

**Free Tier Setup (Single Model)**

```bash
# Free tier allows one model at a time
OLLAMA_SERVER_URL=https://ollama.com
OLLAMA_SERVER_API_KEY=your_ollama_cloud_api_key
OLLAMA_SERVER_MODEL=gpt-oss:120b  # Example: OpenAI OSS 120B model
```

**Paid Tier Setup (Multi-Model with Pre-built Configuration)**

For paid tiers supporting concurrent requests and pay-as-you-go credits, use the pre-built Ollama Cloud configuration:

```bash
# Using pre-built Ollama Cloud configuration (included in Docker image)
OLLAMA_SERVER_URL=https://ollama.com
OLLAMA_SERVER_API_KEY=your_ollama_cloud_api_key
OLLAMA_SERVER_CONFIG_PATH=/opt/pentagi/conf/ollama-cloud.provider.yml
```

The pre-built `ollama-cloud.provider.yml` configuration includes optimized model assignments for all agent types:
- **Simple/Reflector/Enricher**: `minimax-m2.7:cloud` - Reliable low-latency utility model
- **Simple JSON/Primary Agent/Assistant/Researcher/Searcher**: `deepseek-v4.1-flash:cloud` - Efficient long-context model at the same input/output rate as MiniMax M2.7
- **Generator/Adviser**: `kimi-k3:cloud` - Flagship long-horizon planning and knowledge-work model
- **Refiner**: `glm-5.3:cloud` - Flagship coding and agentic model
- **Coder/Installer**: `kimi-k2.7-code:cloud` - Coding-specialized long-context model

**Custom Configuration (Advanced)**

To create your own agent configuration, mount a custom file from your host filesystem:

```bash
# Using custom provider configuration
OLLAMA_SERVER_URL=https://ollama.com
OLLAMA_SERVER_API_KEY=your_ollama_cloud_api_key
OLLAMA_SERVER_CONFIG_PATH=/opt/pentagi/conf/ollama.provider.yml

# Mount custom configuration from host filesystem (in .env or docker-compose override)
PENTAGI_OLLAMA_SERVER_CONFIG_PATH=/path/on/host/my-ollama-config.yml
```

The `PENTAGI_OLLAMA_SERVER_CONFIG_PATH` environment variable maps your host configuration file to `/opt/pentagi/conf/ollama.provider.yml` inside the container.

**Example custom configuration** (`my-ollama-config.yml`):

```yaml
primary_agent:
  model: "deepseek-v4-flash:cloud"
  temperature: 1.0
  top_p: 0.95
  max_tokens: 32768

coder:
  model: "kimi-k2.7-code:cloud"
  temperature: 1.0
  max_tokens: 20480
```

#### Local Ollama Configuration

For self-hosted Ollama instances:

```bash
# Basic local Ollama setup
OLLAMA_SERVER_URL=http://localhost:11434
OLLAMA_SERVER_MODEL=llama3.1:8b-instruct-q8_0

# Production setup with auto-pull and model discovery
OLLAMA_SERVER_URL=http://ollama-server:11434
OLLAMA_SERVER_PULL_MODELS_ENABLED=true
OLLAMA_SERVER_PULL_MODELS_TIMEOUT=900
OLLAMA_SERVER_LOAD_MODELS_ENABLED=true

# Using pre-built configurations from Docker image
OLLAMA_SERVER_CONFIG_PATH=/opt/pentagi/conf/ollama-llama318b.provider.yml
# or
OLLAMA_SERVER_CONFIG_PATH=/opt/pentagi/conf/ollama-qwen332b-fp16-tc.provider.yml
# or
OLLAMA_SERVER_CONFIG_PATH=/opt/pentagi/conf/ollama-qwq32b-fp16-tc.provider.yml
```

**Performance Considerations:**

- **Model Discovery** (`OLLAMA_SERVER_LOAD_MODELS_ENABLED=true`): Adds 1-2s startup latency querying Ollama API
- **Auto-pull** (`OLLAMA_SERVER_PULL_MODELS_ENABLED=true`): First startup may take several minutes downloading models
- **Pull timeout** (`OLLAMA_SERVER_PULL_MODELS_TIMEOUT=900`): 15 minutes in seconds
- **Static Config**: Disable both flags and specify models in config file for fastest startup

#### Creating Custom Ollama Models with Extended Context

KeLai AGI requires models with larger context windows than the default Ollama configurations. You need to create custom models with increased `num_ctx` parameter through Modelfiles. While typical agent workflows consume around 64K tokens, KeLai AGI uses 110K context size for safety margin and long multi-step tasks.

**Important**: The `num_ctx` parameter can only be set during model creation via Modelfile - it cannot be changed after model creation or overridden at runtime.

##### Example: Qwen3 32B FP16 with Extended Context

Create a Modelfile named `Modelfile_qwen3_32b_fp16_tc`:

```dockerfile
FROM qwen3:32b-fp16
PARAMETER num_ctx 110000
PARAMETER temperature 0.3
PARAMETER top_p 0.8
PARAMETER min_p 0.0
PARAMETER top_k 20
PARAMETER repeat_penalty 1.1
```

Build the custom model:

```bash
ollama create qwen3:32b-fp16-tc -f Modelfile_qwen3_32b_fp16_tc
```

##### Example: QwQ 32B FP16 with Extended Context

Create a Modelfile named `Modelfile_qwq_32b_fp16_tc`:

```dockerfile
FROM qwq:32b-fp16
PARAMETER num_ctx 110000
PARAMETER temperature 0.2
PARAMETER top_p 0.7
PARAMETER min_p 0.0
PARAMETER top_k 40
PARAMETER repeat_penalty 1.2
```

Build the custom model:

```bash
ollama create qwq:32b-fp16-tc -f Modelfile_qwq_32b_fp16_tc
```

> **Note**: The QwQ 32B FP16 model requires approximately **71.3 GB VRAM** for inference. Ensure your system has sufficient GPU memory before attempting to use this model.

These custom models are referenced in the pre-built provider configuration files (`ollama-qwen332b-fp16-tc.provider.yml` and `ollama-qwq32b-fp16-tc.provider.yml`) that are included in the Docker image at `/opt/pentagi/conf/`.

### OpenAI Provider Configuration

KeLai AGI integrates with OpenAI's comprehensive model lineup, featuring advanced reasoning capabilities with extended chain-of-thought, agentic models with enhanced tool integration, and specialized code models.

#### Configuration Variables

| Variable             | Default                     | Description                 |
| -------------------- | --------------------------- | --------------------------- |
| `OPEN_AI_KEY`        |                             | API key for OpenAI services |
| `OPEN_AI_SERVER_URL` | `https://api.openai.com/v1` | OpenAI API endpoint         |

#### Configuration Examples

```bash
# Basic OpenAI setup
OPEN_AI_KEY=your_openai_api_key
OPEN_AI_SERVER_URL=https://api.openai.com/v1

# Using with a proxy
OPEN_AI_KEY=your_openai_api_key
PROXY_URL=http://your-proxy:8080
```

#### Supported Models

KeLai AGI supports 32 OpenAI models with tool calling, streaming, reasoning modes, and prompt caching. Models marked with `*` are used in default configuration. Models marked `⚠️` are deprecated by OpenAI and kept only for backward compatibility with agent configs already pinned to those names — avoid them for new assignments.

**GPT-5.6 Series - Latest Frontier (Feb 2026 knowledge cutoff, 1.05M context, 128K max output)**

| Model ID              | Thinking | Reasoning Effort            | Price (Input/Output/Cache) | Use Case                                        |
| --------------------- | -------- | ---------------------------- | -------------------------- | ----------------------------------------------- |
| `gpt-5.6-sol`         | ✅        | low/medium/high/xhigh        | $5.00/$30.00/$0.50         | Frontier model for complex professional work and the most demanding long agentic tasks |
| `gpt-5.6-terra`*      | ✅        | low/medium/high/xhigh        | $2.50/$15.00/$0.25         | Balances intelligence and cost; multi-step planning and coordinated tool use (generator/refiner/adviser/coder default) |
| `gpt-5.6-luna`        | ✅        | low/medium/high/xhigh        | $1.00/$6.00/$0.10          | Optimized for cost-sensitive, high-volume workloads; bulk reading and classification, real-time monitoring |

**GPT-5.5 Series - Frontier (Dec 2025 knowledge cutoff, 1.05M context, 128K max output)**

| Model ID              | Thinking | Reasoning Effort              | Price (Input/Output/Cache) | Use Case                                        |
| --------------------- | -------- | ------------------------------ | -------------------------- | ----------------------------------------------- |
| `gpt-5.5`             | ✅        | none/low/medium/high/xhigh     | $5.00/$30.00/$0.50         | New class of intelligence for coding and professional work; complex research and long autonomous tasks |
| `gpt-5.5-pro`         | ✅        | medium/high/xhigh              | $30.00/$180.00/$0.00       | Uses more compute for smarter, more precise responses; no cached-input discount; mission-critical research |

**GPT-5.4 Series - Advanced Reasoning at Scale (1M context)**

| Model ID              | Thinking | Reasoning Effort         | Price (Input/Output/Cache) | Use Case                                        |
| --------------------- | -------- | -------------------------- | -------------------------- | ----------------------------------------------- |
| `gpt-5.4`             | ✅        | low/medium/high/xhigh      | $2.50/$15.00/$0.25          | Best intelligence at scale for agentic, coding, and professional workflows; maximum depth for multi-step tasks |
| `gpt-5.4-mini`*       | ✅        | low/medium/high/xhigh      | $0.75/$4.50/$0.075          | Strongest mini model for coding, computer use, subagents (primary_agent/assistant/reflector/installer/pentester default) |
| `gpt-5.4-nano`*       | ✅        | low/medium/high/xhigh      | $0.20/$1.25/$0.02           | Cheapest GPT-5.4-class model for simple, high-volume tasks (simple/simple_json/searcher/enricher default) |

**GPT-5.2 Series - Previous Flagship Agentic**

| Model ID              | Thinking | Reasoning Effort       | Price (Input/Output/Cache) | Use Case                                        |
| --------------------- | -------- | ------------------------ | -------------------------- | ----------------------------------------------- |
| `gpt-5.2`             | ✅        | low/medium/high/xhigh    | $1.75/$14.00/$0.175        | Superseded by 5.4/5.6; autonomous research and complex multi-step tasks |
| `gpt-5.2-pro`         | ✅        | medium/high/xhigh        | $21.00/$168.00/$0.00       | Superior agentic coding and long-context performance, mission-critical research |

**GPT-5/5.1 Series - Advanced Agentic Models**

| Model ID              | Thinking | Price (Input/Output/Cache) | Use Case                                        |
| --------------------- | -------- | -------------------------- | ----------------------------------------------- |
| `gpt-5`               | ✅        | $1.25/$10.00/$0.125        | Autonomous research and coordinating multi-tool workflows |
| `gpt-5.1`             | ✅        | $1.25/$10.00/$0.125        | Bridges GPT-5 and GPT-5.2 with faster responses; balanced agentic work with strong tool coordination |
| `gpt-5-pro`           | ✅ (high) | $15.00/$120.00/$0.00       | Reduced hallucinations, exceptional accuracy, critical workloads |
| `gpt-5-mini`          | ✅        | $0.25/$2.00/$0.025         | Automated analysis with strong function calling |
| `gpt-5-nano`          | ✅        | $0.05/$0.40/$0.005         | High-throughput classification and real-time monitoring |

**GPT-4.1 Series - Enhanced Intelligence (Non-Reasoning)**

| Model ID              | Thinking | Price (Input/Output/Cache) | Use Case                                        |
| --------------------- | -------- | -------------------------- | ----------------------------------------------- |
| `gpt-4.1`             | ❌        | $2.00/$8.00/$0.50          | Superior function calling, complex analysis and long tool chains |
| `gpt-4.1-mini`        | ❌        | $0.40/$1.60/$0.10          | Routine analysis and automated code tasks (no longer used in default configuration) |

**GPT-4o Series - Multimodal (Non-Reasoning)**

| Model ID              | Thinking | Price (Input/Output/Cache) | Use Case                                        |
| --------------------- | -------- | -------------------------- | ----------------------------------------------- |
| `gpt-4o-mini`         | ❌        | $0.15/$0.60/$0.075         | Compact multimodal with strong function calling, high-frequency calls, cost-effective bulk operations |

**o-Series - Advanced Reasoning Models (Current)**

| Model ID              | Thinking | Price (Input/Output/Cache) | Use Case                                        |
| --------------------- | -------- | -------------------------- | ----------------------------------------------- |
| `o3`                  | ✅        | $2.00/$8.00/$0.50          | Succeeded by GPT-5; multi-stage reasoning and deep analysis |
| `o3-pro`              | ✅        | $20.00/$80.00/$0.00        | More compute for better responses; critical research |

**Deprecated Models - Kept for Backward Compatibility ⚠️**

These models were marked deprecated by OpenAI. KeLai AGI keeps them defined only so that pre-existing agent configs pinned to these names keep working; do not assign them to new agents.

| Model ID              | Thinking | Price (Input/Output/Cache) | Notes                                        |
| --------------------- | -------- | -------------------------- | --------------------------------------------- |
| `gpt-5.2-codex`       | ✅        | $1.75/$14.00/$0.175        | Superseded code-specialized model; use `gpt-5.6-terra`/`gpt-5.4-mini` instead |
| `gpt-5.1-codex-max`   | ✅        | $1.25/$10.00/$0.125        | Superseded; enhanced reasoning for coding workflows |
| `gpt-5.1-codex`       | ✅        | $1.25/$10.00/$0.125        | Superseded standard code-optimized model |
| `gpt-5-codex`         | ✅        | $1.25/$10.00/$0.125        | Superseded foundational code-specialized model |
| `gpt-5.1-codex-mini`  | ✅        | $0.25/$2.00/$0.025         | Superseded compact code model |
| `codex-mini-latest`   | ✅        | $1.50/$6.00/$0.375         | Superseded compact code model |
| `gpt-4o`              | ❌        | $2.50/$10.00/$1.25         | Superseded by GPT-5.x/5.6 series multimodal flagship |
| `gpt-4.1-nano`        | ❌        | $0.10/$0.40/$0.025         | Superseded ultra-fast lightweight model |
| `o3-mini`             | ✅        | $1.10/$4.40/$0.55          | Superseded compact reasoning model |
| `o4-mini`             | ✅        | $1.10/$4.40/$0.275         | Succeeded by `gpt-5-mini` |
| `o1`                  | ✅        | $15.00/$60.00/$7.50        | Superseded premier reasoning model |
| `o1-pro`              | ✅        | $150.00/$600.00/$0.00      | Superseded, highest cost point of the o-series |

**Prices**: Per 1M tokens. Reasoning models include thinking tokens in output pricing.

> [!WARNING]
> **GPT-5/5.1/5.2 Models**
>
> The original GPT-5, GPT-5.1, and GPT-5.2 models (`gpt-5`, `gpt-5.1`, `gpt-5.2`, `gpt-5-pro`, `gpt-5.2-pro`, and all deprecated Codex variants) work **unstably with KeLai AGI** and may trigger OpenAI's safety mechanisms without verified access. This does not affect the newer GPT-5.4/5.5/5.6 series used in KeLai AGI's default configuration below.
>
> **Recommended alternatives:**
> - Use KeLai AGI's defaults — `gpt-5.4-mini`/`gpt-5.4-nano`/`gpt-5.6-terra` — which work out of the box
> - Use `o3`/`o3-pro` for reasoning tasks
> - Use `gpt-4.1` series for general intelligence and function calling without reasoning

**Reasoning Configuration**:
- **Reasoning forced off by default**: every default agent assigned `gpt-5.4-mini` or `gpt-5.6-terra` (primary_agent, assistant, generator, refiner, adviser, reflector, coder, installer, pentester) sets `reasoning: {mode: off}` — this genuinely disables reasoning, it is not simply "low effort". KeLai AGI calls OpenAI exclusively through `/v1/chat/completions` (never `/v1/responses`), and this endpoint rejects requests that combine function tools with these models' default-on thinking; forcing thinking off is required for tool calls to work reliably (see the investigation notes in [`backend/pkg/providers/openai/config.yml`](../backend/pkg/providers/openai/config.yml)).
- **No override needed for `gpt-5.4-nano`**: used for simple, simple_json, searcher, and enricher, this tier does not default to thinking on, so tools attach without conflict and no `reasoning` override is required.
- **Manual tuning available**: outside the default assignments, GPT-5.6/5.5/5.4/5.2 series models expose explicit reasoning effort levels (`low`/`medium`/`high`/`xhigh`, plus `none` on GPT-5.5) for custom agent configs that need variable reasoning depth with tool calling disabled or via `/v1/responses`.

**Key Features**:
- **Extended Reasoning**: GPT-5.4/5.5/5.6 and o-series models with chain-of-thought for complex analysis
- **Agentic Intelligence**: GPT-5.4/5.5/5.6 series with enhanced tool integration, million-token context windows, and autonomous capabilities
- **Prompt Caching**: Cost reduction on repeated context (10-50% of input price)
- **Code Specialization**: Legacy Codex models remain available (deprecated) in pinned configs
- **Multimodal Support**: `gpt-4o-mini` for vision tasks such as screenshot reading
- **Tool Calling**: Robust function calling across all models for tool orchestration
- **Streaming**: Real-time response streaming for interactive workflows

### Anthropic Provider Configuration

KeLai AGI integrates with Anthropic's Claude models, featuring advanced extended thinking capabilities, exceptional safety mechanisms and prompt caching.

#### Configuration Variables

| Variable               | Default                        | Description                    |
| ---------------------- | ------------------------------ | ------------------------------ |
| `ANTHROPIC_API_KEY`    |                                | API key for Anthropic services |
| `ANTHROPIC_SERVER_URL` | `https://api.anthropic.com/v1` | Anthropic API endpoint         |

#### Configuration Examples

```bash
# Basic Anthropic setup
ANTHROPIC_API_KEY=your_anthropic_api_key
ANTHROPIC_SERVER_URL=https://api.anthropic.com/v1

# Using with proxy for secure environments
ANTHROPIC_API_KEY=your_anthropic_api_key
PROXY_URL=http://your-proxy:8080

# Workload Identity Federation instead of a long-lived key (leave ANTHROPIC_API_KEY empty)
ANTHROPIC_FEDERATION_RULE_ID=fdrl_...
ANTHROPIC_ORGANIZATION_ID=00000000-0000-0000-0000-000000000000
ANTHROPIC_SERVICE_ACCOUNT_ID=svac_...
ANTHROPIC_WORKSPACE_ID=wrkspc_...
ANTHROPIC_IDENTITY_TOKEN_FILE=/var/run/secrets/anthropic.com/token
```

With [Workload Identity Federation](https://platform.claude.com/docs/en/manage-claude/workload-identity-federation), KeLai AGI exchanges the identity token your platform issues (Kubernetes, GitHub Actions, cloud IAM, any OIDC issuer) for a short-lived Claude API token and refreshes it on its own. The token file is re-read on every exchange and must be mounted into the pentagi container. A token carrying a `jti` claim, as Kubernetes and GitHub Actions tokens do, can be exchanged only once, so the file must hold a new token before each refresh: rotate it well within the lifetime of the minted token. `ANTHROPIC_IDENTITY_TOKEN` takes the token itself where the platform injects it as a variable, but it is read once and cannot be rotated, so it suits only runs shorter than the identity token's lifetime. A set `ANTHROPIC_API_KEY` wins over federation. See [backend/docs/config.md](../backend/docs/config.md#workload-identity-federation) for every variable.

> [!NOTE]
> **Google Vertex AI for Claude models**
>
> KeLai AGI does not currently expose a dedicated Google Vertex AI configuration path for Anthropic Claude in `.env`. There is no separate Vertex AI API key field at this time, and the existing Anthropic variables (`ANTHROPIC_API_KEY`, `ANTHROPIC_SERVER_URL`) target the direct Anthropic API. Supported routes for Claude are:
>
> - **Direct Anthropic API**: `ANTHROPIC_API_KEY` and `ANTHROPIC_SERVER_URL` (see above).
> - **AWS Bedrock**: `BEDROCK_*` variables (see [AWS Bedrock Provider Configuration](#aws-bedrock-provider-configuration)).
>
> If you need to use Vertex AI today, the safest supported workaround is to expose Vertex AI through an OpenAI-compatible proxy or gateway that translates Vertex AI calls into the Chat Completions format while preserving the chat and tool-call behavior KeLai AGI relies on, then point the Custom LLM provider at that gateway via `LLM_SERVER_URL`, `LLM_SERVER_KEY`, and `LLM_SERVER_MODEL`. This path is only as reliable as the gateway you choose.

#### Supported Models

KeLai AGI supports 9 Claude models with tool calling, streaming, extended thinking, adaptive thinking, and prompt caching. Models marked with `*` are used in default configuration.

**Claude 5 Series - Newest Models (2026)**

| Model ID                 | Thinking | Release Date | Price (Input/Output/Cache R/W) | Use Case                                        |
| ------------------------ | -------- | ------------ | ------------------------------ | ----------------------------------------------- |
| `claude-sonnet-5`*       | ✅        | Jul 2026     | $3.00/$15.00/$0.30/$3.75       | Best combination of speed and intelligence for coding, agents, and professional work at scale. Adaptive thinking only (manual budget thinking rejected); sampling parameters not supported. Default model for primary agent, assistant, coder, adviser, installer, pentester |
| `claude-fable-5`         | ✅        | Jun 2026     | $10.00/$50.00/$1.00/$12.50     | Anthropic's most capable widely released model for long-running agents and the most demanding reasoning workloads. Adaptive thinking always on (budget thinking and an explicit disable are rejected); sampling parameters not supported |

**Claude 4 Series**

| Model ID                 | Thinking | Release Date | Price (Input/Output/Cache R/W) | Use Case                                        |
| ------------------------ | -------- | ------------ | ------------------------------ | ----------------------------------------------- |
| `claude-opus-4-8`*       | ✅        | May 2026     | $5.00/$25.00/$0.50/$6.25       | Flagship for coding, agents, and deep reasoning in enterprise workflows. Adaptive thinking only — budget thinking and sampling params (temperature/top_p/top_k) are rejected. Default model for generator and refiner; the most demanding planning |
| `claude-opus-4-7`        | ✅        | Apr 2026     | $5.00/$25.00/$0.50/$6.25       | Advanced software engineering and long-running agentic work. Adaptive thinking only (manual budget thinking rejected) |
| `claude-sonnet-4-6`      | ✅        | Feb 2026     | $3.00/$15.00/$0.30/$3.75       | Best speed/intelligence balance with adaptive thinking. Multi-step analysis and real-time monitoring |
| `claude-opus-4-6`        | ✅        | Feb 2026     | $5.00/$25.00/$0.50/$6.25       | Most intelligent model for autonomous agents and coding. Extended + adaptive thinking for complex multi-stage tasks |
| `claude-haiku-4-5`*      | ❌        | Oct 2025     | $1.00/$5.00/$0.10/$1.25        | Fast and efficient model with exceptional function calling and low latency, no thinking support. Default model for simple, simple_json, reflector, searcher, enricher; high-frequency calls, real-time monitoring, bulk automated work |

**Legacy Models - Still Supported**

| Model ID                 | Thinking | Release Date | Price (Input/Output/Cache R/W) | Use Case                                        |
| ------------------------ | -------- | ------------ | ------------------------------ | ----------------------------------------------- |
| `claude-sonnet-4-5`      | ✅        | Sep 2025     | $3.00/$15.00/$0.30/$3.75       | State-of-the-art reasoning (superseded by sonnet-4-6/sonnet-5). Sophisticated agentic work and analysis |
| `claude-opus-4-5`        | ✅        | Nov 2025     | $5.00/$25.00/$0.50/$6.25       | Ultimate reasoning (superseded by opus-4-6/4-7/4-8). Critical research and long agentic tasks |

**Prices**: Per 1M tokens. Cache pricing includes both Read and Write costs.

**Extended Thinking Configuration** (default agent config, see [`backend/pkg/providers/anthropic/config.yml`](../backend/pkg/providers/anthropic/config.yml)):
- **Generator / Refiner** (`claude-opus-4-8`): adaptive reasoning at `xhigh`/`high` effort for maximum reasoning depth on complex planning
- **Primary agent, assistant, coder, adviser, installer, pentester** (`claude-sonnet-5`): adaptive reasoning (adviser at `xhigh` effort) for balanced code analysis and research
- **Reflector, searcher** (`claude-haiku-4-5`): fixed reasoning budget of 1024 tokens for focused reasoning on specific tasks
- **Simple, simple_json, enricher** (`claude-haiku-4-5`): no thinking, optimized for speed

**Key Features**:
- **Extended Thinking**: All Claude 4.5+ models support configurable chain-of-thought reasoning depths for complex analysis
- **Adaptive Thinking**: Claude 4.6 series (Opus/Sonnet) dynamically adjusts reasoning depth based on task complexity; Claude Opus 4.7/4.8 and the Claude 5 series (Sonnet/Fable) are adaptive-thinking-only (manual budget thinking and sampling parameters are rejected with HTTP 400)
- **Prompt Caching**: Significant cost reduction with separate read/write pricing (10% read, 125% write of input)
- **Extended Context Window**: 200K tokens standard, up to 1M tokens (beta) for Claude Opus/Sonnet 4.6 for comprehensive codebase analysis
- **Tool Calling**: Robust function calling with exceptional accuracy for tool orchestration
- **Streaming**: Real-time response streaming for interactive workflows
- **Multimodal Support**: Vision capabilities in latest models for screenshot analysis

### Google AI (Gemini) Provider Configuration

KeLai AGI integrates with Google's Gemini models through the Google AI API, offering state-of-the-art multimodal reasoning capabilities with extended thinking and context caching.

#### Configuration Variables

| Variable            | Default                                     | Description                    |
| ------------------- | ------------------------------------------- | ------------------------------ |
| `GEMINI_API_KEY`    |                                             | API key for Google AI services |
| `GEMINI_SERVER_URL` | `https://generativelanguage.googleapis.com` | Google AI API endpoint         |

#### Configuration Examples

```bash
# Basic Gemini setup
GEMINI_API_KEY=your_gemini_api_key
GEMINI_SERVER_URL=https://generativelanguage.googleapis.com

# Using with proxy
GEMINI_API_KEY=your_gemini_api_key
PROXY_URL=http://your-proxy:8080
```

#### Supported Models

KeLai AGI lists 14 Gemini model IDs that responded through the tested API endpoint. Availability in the catalogue does not mean a model is suitable for every agent role: the saved-chain results below determine the bundled defaults. Models marked with `*` are used in `config.yml`.

| Model ID | Thinking | Context | Price (Input/Output/Cache) | Saved-chain result |
| --- | --- | --- | --- | --- |
| `gemini-3.8-flash` | ✅ | 1M | $0.75/$3.75/$0.075 | API responds; 10/10 saved generator calls were content-filtered |
| `gemini-3.7-flash` | ✅ | 1M | $0.75/$3.75/$0.075 | API responds; 10/10 saved generator calls were content-filtered; former default |
| `gemini-3.6-flash` | ✅ | 1M | $0.75/$3.75/$0.075 | API responds; 10/10 saved generator calls returned text without a tool |
| `gemini-3.5-flash` | ✅ | 1M | $1.5/$9/$0.15 | API responds; 10/10 saved generator calls returned text without a tool |
| `gemini-3.5-flash-lite`* | ✅ | 1M | $0.3/$2.5/$0.03 | Complex-role default; eight direct plans and two after a safe `memorist` continuation |
| `gemini-3.1-pro-preview` | ✅ | 1M | $2/$12/$0.2 | API responds; 10/10 saved generator calls returned text without a tool |
| `gemini-3.1-pro-preview-customtools` | ✅ | 1M | $2/$12/$0.2 | API responds; 10/10 saved generator calls returned text without a tool |
| `gemini-3.1-flash-lite`* | ✅ | 1M | $0.25/$1.5/$0.025 | Simple-role default; direct plan on all ten saved generator chains |
| `gemini-3-flash-preview` | ✅ | 1M | $0.5/$3/$0.05 | API responds; three direct plans and five generator timeouts |
| `gemini-2.5-pro` | ✅ | 1M | $1.25/$10/$0.125 | API responds; ten direct plans, but four role-smoke provider errors |
| `gemini-2.5-flash` | ✅ | 1M | $0.3/$2.5/$0.03 | Ten direct plans; access is limited for new API projects |
| `gemini-2.5-flash-lite` | ✅ | 1M | $0.1/$0.4/$0.01 | API responds; five of ten generator answers were empty |
| `gemma-4-31b-it` | ✅ | 256K | Free/Free/Free | API responds; seven direct plans and three generator timeouts |
| `gemma-4-26b-a4b-it` | ✅ | 256K | Free/Free/Free | Direct plan on all ten saved generator chains |

**Prices**: Per 1M tokens. Google [limits Gemini 2.5 access for new projects](https://ai.google.dev/gemini-api/docs/models/gemini-2.5-pro), so KeLai AGI uses the tested 3.1 Flash-Lite as its fallback and simple-role default. Existing custom 2.5 configurations remain usable where the project has access.

**Default Model Assignments (config.yml)**:
- **`gemini-3.1-flash-lite`** - `simple`, `simple_json`, `reflector`, `searcher`, `enricher`
- **`gemini-3.5-flash-lite`** - `primary_agent`, `assistant`, `generator`, `refiner`, `adviser`, `coder`, `installer`, `pentester`

Both default models passed a basic API/configuration smoke for every role. [Google reports stronger agentic and coding performance for 3.5 Flash-Lite than 3.1 Flash-Lite](https://blog.google/innovation-and-ai/models-and-research/gemini-models/gemini-3-6-flash-3-5-flash-lite-3-5-flash-cyber/); KeLai AGI's saved production chains cover generator and reflector only, so the smoke does not establish semantic quality or multi-turn reliability for other roles. On two saved generator chains, 3.5 Flash-Lite first chose the allowed `memorist` tool and called `subtask_list` after a synthetic tool response; no real tool was executed in that continuation.

**Reasoning Effort Levels**:
- **High**: Deeper reasoning for planning and review (`primary_agent`, `generator`, `refiner`, `adviser`)
- **Medium**: Balanced reasoning for execution roles (`assistant`, `coder`, `installer`, `pentester`)
- **Not set**: No thinking setting is sent and the model's own default applies (`simple`, `simple_json`, `reflector`, `searcher`, `enricher`)

### AWS Bedrock Provider Configuration

KeLai AGI integrates with Amazon Bedrock, offering access to 20+ foundation models from leading AI companies including Anthropic, Amazon, Cohere, DeepSeek, OpenAI, Qwen, Mistral, and Moonshot.

#### Configuration Variables

| Variable                    | Default     | Description                                                                                         |
| --------------------------- | ----------- | --------------------------------------------------------------------------------------------------- |
| `BEDROCK_REGION`            | `us-east-1` | AWS region for Bedrock service                                                                      |
| `BEDROCK_DEFAULT_AUTH`      | `false`     | Use AWS SDK default credential chain (environment, EC2 role, ~/.aws/credentials) - highest priority |
| `BEDROCK_BEARER_TOKEN`      |             | Bearer token authentication - priority over static credentials                                      |
| `BEDROCK_ACCESS_KEY_ID`     |             | AWS access key ID for static credentials                                                            |
| `BEDROCK_SECRET_ACCESS_KEY` |             | AWS secret access key for static credentials                                                        |
| `BEDROCK_SESSION_TOKEN`     |             | AWS session token for temporary credentials (optional, used with static credentials)                |
| `BEDROCK_SERVER_URL`        |             | Custom Bedrock endpoint (VPC endpoints, local testing)                                              |
| `BEDROCK_CONFIG_PATH`       |             | Path to a custom YAML provider config file (overrides the built-in default config for model/pricing definitions) |

**Authentication Priority**: `BEDROCK_DEFAULT_AUTH` → `BEDROCK_BEARER_TOKEN` → `BEDROCK_ACCESS_KEY_ID`+`BEDROCK_SECRET_ACCESS_KEY`

#### Configuration Examples

```bash
# Recommended: Default AWS SDK authentication (EC2/ECS/Lambda roles)
BEDROCK_REGION=us-east-1
BEDROCK_DEFAULT_AUTH=true

# Bearer token authentication (AWS STS, custom auth)
BEDROCK_REGION=us-east-1
BEDROCK_BEARER_TOKEN=your_bearer_token

# Static credentials (development, testing)
BEDROCK_REGION=us-east-1
BEDROCK_ACCESS_KEY_ID=your_aws_access_key
BEDROCK_SECRET_ACCESS_KEY=your_aws_secret_key

# With proxy and custom endpoint
BEDROCK_REGION=us-east-1
BEDROCK_DEFAULT_AUTH=true
BEDROCK_SERVER_URL=https://bedrock-runtime.us-east-1.vpce-xxx.amazonaws.com
PROXY_URL=http://your-proxy:8080
```

#### Custom Provider Config and Models (advanced)

By default the Bedrock provider uses a per-agent config and model catalog compiled into the binary. One optional path overrides them without rebuilding:

- `BEDROCK_CONFIG_PATH` — a YAML file (same shape as the other provider configs) that **replaces** the built-in per-agent model/price assignments. See [`examples/configs/bedrock-glm-flash.provider.yml`](../examples/configs/bedrock-glm-flash.provider.yml).

This is useful to expose a Bedrock model newer than the compiled-in catalog — for example Z.AI's `zai.glm-4.7-flash`. Use the exact Model ID from the model's AWS Bedrock detail page; add a `us.`/`eu.`/`apac.` inference-profile prefix only when that page marks the model as requiring cross-region inference (`zai.glm-4.7-flash` is In-Region, so it is used as-is, with no prefix).

With Docker Compose the file is mounted from the host at `/opt/pentagi/conf/bedrock.provider.yml`. The default source is `./example.bedrock.provider.yml` beside `docker-compose.yml`, a copy of the built-in config ([`examples/configs/bedrock.provider.yml`](../examples/configs/bedrock.provider.yml)) that the installer extracts, so editing it and pointing the backend at the mounted path is enough:

```bash
# tell the backend to read the mounted file
BEDROCK_CONFIG_PATH=/opt/pentagi/conf/bedrock.provider.yml

# optional: mount another file from the host instead of ./example.bedrock.provider.yml
PENTAGI_BEDROCK_CONFIG_PATH=/path/on/host/my-bedrock.provider.yml
```

#### Supported Models

KeLai AGI supports 24 AWS Bedrock models with tool calling, streaming, and multimodal capabilities. Models marked with `*` are used in default configuration.

| Model ID                                         | Provider        | Thinking | Multimodal | Price (Input/Output) | Use Case                                |
| ------------------------------------------------ | --------------- | -------- | ---------- | -------------------- | --------------------------------------- |
| `us.amazon.nova-2-lite-v1:0`                     | Amazon Nova     | ❌        | ✅          | $0.33/$2.75          | Adaptive reasoning, efficient thinking  |
| `us.amazon.nova-premier-v1:0`                    | Amazon Nova     | ❌        | ✅          | $2.50/$12.50         | Complex reasoning, advanced analysis    |
| `us.amazon.nova-pro-v1:0`                        | Amazon Nova     | ❌        | ✅          | $0.80/$3.20          | Balanced accuracy, speed, cost          |
| `us.amazon.nova-lite-v1:0`                       | Amazon Nova     | ❌        | ✅          | $0.06/$0.24          | Fast processing, high-volume operations |
| `us.amazon.nova-micro-v1:0`                      | Amazon Nova     | ❌        | ❌          | $0.035/$0.14         | Ultra-low latency, real-time monitoring |
| `us.anthropic.claude-opus-4-8`                   | Anthropic       | ✅        | ✅          | $5.00/$25.00         | Flagship coding/agents/deep reasoning; adaptive thinking only (sampling params rejected) |
| `us.anthropic.claude-opus-4-7`                   | Anthropic       | ✅        | ✅          | $5.00/$25.00         | Advanced engineering, long-running agents; adaptive thinking only |
| `us.anthropic.claude-opus-4-6-v1`*               | Anthropic       | ✅        | ✅          | $5.00/$25.00         | World-class coding, enterprise agents   |
| `us.anthropic.claude-sonnet-4-6`                 | Anthropic       | ✅        | ✅          | $3.00/$15.00         | Frontier intelligence, enterprise scale |
| `us.anthropic.claude-opus-4-5-20251101-v1:0`     | Anthropic       | ✅        | ✅          | $5.00/$25.00         | Multi-day software development          |
| `us.anthropic.claude-haiku-4-5-20251001-v1:0`*   | Anthropic       | ✅        | ✅          | $1.00/$5.00          | Near-frontier performance, high speed   |
| `us.anthropic.claude-sonnet-4-5-20250929-v1:0`*  | Anthropic       | ✅        | ✅          | $3.00/$15.00         | Real-world agents, coding excellence    |
| `us.anthropic.claude-sonnet-4-20250514-v1:0`     | Anthropic       | ✅        | ✅          | $3.00/$15.00         | Balanced performance, production-ready  |
| `us.anthropic.claude-3-5-haiku-20241022-v1:0`    | Anthropic       | ❌        | ❌          | $0.80/$4.00          | Fastest model, cost-effective bulk calls  |
| `cohere.command-r-plus-v1:0`                     | Cohere          | ❌        | ❌          | $3.00/$15.00         | Large-scale operations, superior RAG    |
| `deepseek.v3.2`                                  | DeepSeek        | ❌        | ❌          | $0.58/$1.68          | Long-context reasoning, efficiency      |
| `openai.gpt-oss-120b-1:0`*                       | OpenAI (OSS)    | ✅        | ❌          | $0.15/$0.60          | Strong reasoning, scientific analysis   |
| `openai.gpt-oss-20b-1:0`                         | OpenAI (OSS)    | ✅        | ❌          | $0.07/$0.30          | Efficient coding, software development  |
| `qwen.qwen3-next-80b-a3b`                        | Qwen            | ❌        | ❌          | $0.15/$1.20          | Ultra-long context, flagship reasoning  |
| `qwen.qwen3-32b-v1:0`                            | Qwen            | ❌        | ❌          | $0.15/$0.60          | Balanced reasoning, research use cases  |
| `qwen.qwen3-coder-30b-a3b-v1:0`                  | Qwen            | ❌        | ❌          | $0.15/$0.60          | Vibe coding, natural-language first     |
| `qwen.qwen3-coder-next`                          | Qwen            | ❌        | ❌          | $0.45/$1.80          | Tool use, function calling optimized    |
| `mistral.mistral-large-3-675b-instruct`          | Mistral         | ❌        | ✅          | $4.00/$12.00         | Advanced multimodal, long-context       |
| `moonshotai.kimi-k2.5`                           | Moonshot        | ❌        | ✅          | $0.60/$3.00          | Vision, language, code in one model     |

**Prices**: Per 1M tokens. Models with thinking/reasoning support additional compute costs during reasoning phase.

#### Tested but Incompatible Models

Some AWS Bedrock models were tested but are **not supported** due to technical limitations:

| Model Family              | Reason for Incompatibility                                                                |
| ------------------------- | ----------------------------------------------------------------------------------------- |
| **GLM (Z.AI)**            | Tool calling format incompatible with Converse API (expects string instead of JSON)       |
| **AI21 Jamba**            | Severe rate limits (1-2 req/min) prevent reliable testing and production use              |
| **Meta Llama 3.3/3.1**    | Unstable tool call result processing, causes unexpected failures in multi-turn workflows  |
| **Mistral Magistral**     | Tool calling not supported by the model                                                   |
| **Moonshot K2-Thinking**  | Unstable streaming behavior with tool calls, unreliable in production                     |
| **Qwen3-VL**              | Unstable streaming with tool calling, multimodal + tools combination fails intermittently |

> [!IMPORTANT]
> **Rate Limits & Quota Management**
>
> Default AWS Bedrock quotas for Claude models are **extremely restrictive** (2-20 requests/minute for new accounts). For production workloads:
>
> 1. **Request quota increases** through AWS Service Quotas console for models you plan to use
> 2. **Use Amazon Nova models** - higher default quotas and excellent performance
> 3. **Enable provisioned throughput** for consistent high-volume testing
> 4. **Monitor usage** - AWS throttles aggressively at quota limits
>
> Without quota increases, expect frequent delays and workflow interruptions.

> [!WARNING]
> **Converse API Requirements**
>
> KeLai AGI uses Amazon Bedrock **Converse API** for unified model access. All supported models require:
>
> - ✅ Converse/ConverseStream API support
> - ✅ Tool use (function calling) for agent workflows
> - ✅ Streaming tool use for real-time feedback
>
> Verify model capabilities at: [AWS Bedrock Model Features](https://docs.aws.amazon.com/bedrock/latest/userguide/conversation-inference-supported-models-features.html)

**Key Features**:
- **Automatic Prompt Caching**: 40-70% cost reduction on repeated context (Claude 4.x models)
- **Extended Thinking**: Step-by-step reasoning for complex analysis (Claude, DeepSeek R1, OpenAI GPT)
- **Multimodal Analysis**: Process screenshots, diagrams, video for comprehensive testing (Nova, Claude, Mistral, Kimi)
- **Tool Calling**: Seamless integration with the agents' tools via function calling
- **Streaming**: Real-time response streaming for interactive workflows

### DeepSeek Provider Configuration

KeLai AGI integrates with DeepSeek, providing access to advanced AI models with strong reasoning, coding capabilities, and context caching at competitive prices.

#### Configuration Variables

| Variable              | Default Value              | Description                                         |
| --------------------- | -------------------------- | --------------------------------------------------- |
| `DEEPSEEK_API_KEY`    |                            | DeepSeek API key for authentication                 |
| `DEEPSEEK_SERVER_URL` | `https://api.deepseek.com` | DeepSeek API endpoint URL                           |
| `DEEPSEEK_PROVIDER`   |                            | Provider prefix for LiteLLM integration (optional)  |

#### Configuration Examples

```bash
# Direct API usage
DEEPSEEK_API_KEY=your_deepseek_api_key
DEEPSEEK_SERVER_URL=https://api.deepseek.com

# With LiteLLM proxy
DEEPSEEK_API_KEY=your_litellm_key
DEEPSEEK_SERVER_URL=http://litellm-proxy:4000
DEEPSEEK_PROVIDER=deepseek  # Adds prefix to model names (deepseek/deepseek-v4-flash) for LiteLLM
```

#### Supported Models

KeLai AGI supports 2 DeepSeek V4 models with tool calling, streaming, hybrid thinking/non-thinking modes, and context caching. Both models think by default and switch to non-thinking mode with `reasoning.mode: off`. Models marked with `*` are used in default configuration.

| Model ID              | Thinking | Max Output | Context | Price (Input/Output/Cache) | Use Case                                             |
| --------------------- | -------- | ---------- | ------- | -------------------------- | ---------------------------------------------------- |
| `deepseek-v4-flash`*  | ✅ hybrid | 384K       | 1M      | $0.14/$0.28/$0.0028        | Utility agents, general dialogue, fast tool calling  |
| `deepseek-v4-pro`*    | ✅ hybrid | 384K       | 1M      | $1.74/$3.48/$0.0145        | Advanced reasoning, complex logic and analysis |

**Prices**: Per 1M tokens. Cache pricing applies to prompt tokens served from cache (input cache hit, reduced to 1/10 of launch price since 2026-04-26). Both models support hybrid thinking — `thinking` mode is enabled by default; set `reasoning.mode: off` (Reasoning Mode Off in the UI) to switch to non-thinking mode for faster/cheaper responses.

> **Pricing Note (deepseek-v4-pro)**: The 75% promotional discount on `deepseek-v4-pro` officially ended on 2026-05-31 15:59 UTC. The prices above reflect the standard post-promotional pricing. If you have legacy configurations using the discounted prices ($0.435/$0.87/$0.003625), update them to the current rates for accurate cost tracking.

> The legacy model names `deepseek-chat` and `deepseek-reasoner` are scheduled
> for deprecation by DeepSeek on 2026-07-24. Existing user configurations
> referencing the legacy names continue to work until then; the defaults above
> use the current V4 names. `deepseek-chat` maps to `deepseek-v4-flash`
> non-thinking mode; `deepseek-reasoner` maps to `deepseek-v4-flash` thinking mode.

**Default Agent Configuration**:

Strategy: prefer `deepseek-v4-flash` (12x cheaper input, 12x cheaper output) as the workhorse for utility/lightweight agents; reserve `deepseek-v4-pro` for complex multi-step reasoning. The `installer` agent runs on Flash with thinking enabled because environment setup tasks (shell commands, config edits) rarely require pro-level reasoning. Run A/B tests on your own workloads before promoting more agents to Pro.

| Agent Role                                  | Default Model        | Thinking | Reasoning Effort | Max Output | Temperature | Top P |
| ------------------------------------------- | -------------------- | -------- | ---------------- | ---------- | ----------- | ----- |
| Generator / Refiner                         | `deepseek-v4-pro`    | Enabled  | High             | 32768      | (auto)      | (auto) |
| Coder                                       | `deepseek-v4-pro`    | Enabled  | High             | 20480      | (auto)      | (auto) |
| Primary Agent / Assistant / Researcher      | `deepseek-v4-pro`    | Enabled  | High             | 16384      | (auto)      | (auto) |
| Adviser (mentor/planner)                    | `deepseek-v4-pro`    | Enabled  | High             | 8192       | (auto)      | (auto) |
| Installer                                   | `deepseek-v4-flash`  | Enabled  | High             | 12288      | (auto)      | (auto) |
| Reflector / Searcher / Enricher             | `deepseek-v4-flash`  | Disabled | —                | 4096       | 0.5         | 0.9   |
| Simple / Simple JSON                        | `deepseek-v4-flash`  | Disabled | —                | 2048       | 0.3         | 0.9   |

> **Note**: DeepSeek ignores `presence_penalty` and `frequency_penalty` on both models. While thinking runs, `temperature` has no effect and langchaingo drops it, but `top_p` is sent and applied (values below 0.95 are raised to 0.95); in non-thinking mode `top_p` is fixed at 1.0, so langchaingo drops it and sends `temperature` instead. The thinking agents leave both unset, shown as "(auto)" in the table above.

**Key Features**:
- **Hybrid Thinking Modes**: Switch between thinking (deep reasoning) and non-thinking (fast) modes via `reasoning.mode`
- **Automatic Prompt Caching**: Significant cost reduction on repeated context via cache-hit pricing (1/10 of launch price)
- **Extended Thinking**: Reinforcement learning CoT for complex analysis (both V4 models)
- **Strong Coding**: Optimized for code generation
- **Long Context**: 1M token context window with up to 384K output tokens
- **Tool Calling**: Seamless integration with the agents' tools via function calling
- **Streaming**: Real-time response streaming for interactive workflows
- **Multilingual**: Strong Chinese and English support
- **Additional Features**: JSON Output, Chat Prefix Completion (beta), FIM/Fill-in-the-Middle Completion (non-thinking mode only)

**Concurrency Limits**: `deepseek-v4-flash`: 2500 concurrent requests; `deepseek-v4-pro`: 500 concurrent requests.

**LiteLLM Integration**: Set `DEEPSEEK_PROVIDER=deepseek` to enable model name prefixing when using default KeLai AGI configurations with LiteLLM proxy. Leave empty for direct API usage.

### GLM Provider Configuration

KeLai AGI integrates with GLM from Zhipu AI (Z.AI), providing advanced language models with MoE architecture, strong reasoning, and agentic capabilities developed by Tsinghua University.

#### Configuration Variables

| Variable          | Default Value                   | Description                                                |
| ----------------- | ------------------------------- | ---------------------------------------------------------- |
| `GLM_API_KEY`     |                                 | GLM API key for authentication                             |
| `GLM_SERVER_URL`  | `https://api.z.ai/api/paas/v4`  | GLM API endpoint URL (international)                       |
| `GLM_PROVIDER`    |                                 | Provider prefix for LiteLLM integration (optional)         |

#### Configuration Examples

```bash
# Direct API usage (international endpoint)
GLM_API_KEY=your_glm_api_key
GLM_SERVER_URL=https://api.z.ai/api/paas/v4

# Alternative endpoints
GLM_SERVER_URL=https://open.bigmodel.cn/api/paas/v4  # China
GLM_SERVER_URL=https://api.z.ai/api/coding/paas/v4   # Coding-specific

# With LiteLLM proxy
GLM_API_KEY=your_litellm_key
GLM_SERVER_URL=http://litellm-proxy:4000
GLM_PROVIDER=zai  # Adds prefix to model names (zai/glm-4) for LiteLLM
```

#### Supported Models

KeLai AGI supports 16 GLM models with tool calling, streaming, reasoning controls, and prompt caching. Models marked with `*` are used in the default configuration. GLM-5.3, GLM-5.3-Flash, and GLM-5.3-FlashX always reason and accept `low`, `high`, or `max` effort.

**GLM-5.x Series - Latest Generation**

| Model ID         | Thinking | Context | Max Output | Price (Input/Output/Cache) | Use Case                                                            |
| ---------------- | -------- | ------- | ---------- | -------------------------- | ------------------------------------------------------------------- |
| `glm-5.3`*       | ✅ Always | 1M      | 128K       | $1.40/$4.40/$0.26          | Current flagship for complex software engineering, agent tasks, and research |
| `glm-5.3-flash`* | ✅ Always | 1M      | 128K       | $0.15/$0.50/$0.03          | Cost-efficient native multimodal model for utility and latency-sensitive agents |
| `glm-5.3-flashx` | ✅ Always | 1M      | 128K       | $0.37/$1.25/$0.075         | Accelerated GLM-5.3-Flash endpoint delivering up to 200 output tokens per second |
| `glm-5.2`        | ✅ Hybrid | 1M      | 128K       | $1.40/$4.40/$0.26          | Previous flagship for long-horizon engineering tasks |
| `glm-5.1`        | ✅ Hybrid | 200K    | 128K       | $1.40/$4.40/$0.26          | Long-horizon tasks: 8h sustained autonomous execution, Claude Opus 4.6-aligned coding |
| `glm-5`          | ✅ Hybrid | 200K    | 128K       | $1.00/$3.20/$0.20          | Foundation for Agentic Engineering, MoE 744B/40B active, Claude Opus 4.5-level coding |

**GLM-4.7 Series - Premium with Interleaved Thinking**

| Model ID          | Thinking | Context | Max Output | Price (Input/Output/Cache) | Use Case                                            |
| ----------------- | -------- | ------- | ---------- | -------------------------- | --------------------------------------------------- |
| `glm-4.7`         | ✅ Hybrid | 200K    | 128K       | $0.60/$2.20/$0.11          | Enhanced programming, stable multi-step reasoning   |
| `glm-4.7-flashx`  | ✅ Hybrid | 200K    | 128K       | $0.07/$0.40/$0.01          | Ultra-cheap with priority GPU, but lower RPM limits (avoid for high-frequency use) |
| `glm-4.7-flash`   | ✅ Hybrid | 200K    | 128K       | Free/Free/Free             | Free ~30B SOTA model, 1 concurrent request          |

**GLM-4.6 Series - Balanced with Auto-Thinking**

| Model ID  | Thinking | Context | Max Output | Price (Input/Output/Cache) | Use Case                                          |
| --------- | -------- | ------- | ---------- | -------------------------- | ------------------------------------------------- |
| `glm-4.6` | ✅ Auto   | 200K    | 128K       | $0.60/$2.20/$0.11          | Balanced, streaming tool calls, token-efficient   |

**GLM-4.5 Series - Unified Reasoning/Coding/Agents**

| Model ID         | Thinking | Context | Max Output | Price (Input/Output/Cache) | Use Case                                          |
| ---------------- | -------- | ------- | ---------- | -------------------------- | ------------------------------------------------- |
| `glm-4.5`        | ✅ Auto   | 128K    | 96K        | $0.60/$2.20/$0.11          | Unified, MoE 355B/32B active                      |
| `glm-4.5-x`      | ✅ Auto   | 128K    | 96K        | $2.20/$8.90/$0.45          | Ultra-fast premium, lowest latency                |
| `glm-4.5-air`    | ✅ Auto   | 128K    | 96K        | $0.20/$1.10/$0.03          | Cost-effective MoE 106B/12B |
| `glm-4.5-airx`   | ✅ Auto   | 128K    | 96K        | $1.10/$4.50/$0.22          | Accelerated Air with priority GPU                 |
| `glm-4.5-flash`  | ✅ Auto   | 128K    | 96K        | Free/Free/Free             | Free with reasoning/coding/agents support         |

**GLM-4 Legacy - Dense Architecture**

| Model ID              | Thinking | Context | Max Output | Price (Input/Output) | Use Case                                      |
| --------------------- | -------- | ------- | ---------- | -------------------- | --------------------------------------------- |
| `glm-4-32b-0414-128k` | ❌        | 128K    | 16K        | $0.10/$0.10          | Ultra-budget dense 32B, parsing without reasoning |

**Prices**: Per 1M tokens. Cache pricing is for prompt cache hit; cache storage is currently free per Z.AI promotion. GLM-4-32B has no cache support.

**Default Agent Configuration**:

Strategy: `glm-5.3` for critical reasoning and `glm-5.3-flash` for inexpensive utility and latency-sensitive work. `glm-5.3-flashx` remains selectable but is not a default because not every compatible gateway deploys it.

| Agent Role                          | Default Model | Thinking | Temperature | Top P | Max Output |
| ----------------------------------- | ------------- | -------- | ----------- | ----- | ---------- |
| Generator / Refiner                 | `glm-5.3`       | Max      | 1.0         | 0.95  | 32768      |
| Coder                               | `glm-5.3`       | Max      | 1.0         | 0.95  | 20480      |
| Adviser / Researcher                | `glm-5.3`       | Max      | 1.0         | 0.95  | 16384      |
| Primary Agent                       | `glm-5.3`       | High     | 1.0         | 0.95  | 16384      |
| Assistant / Installer               | `glm-5.3-flash` | High     | 1.0         | 0.95  | 16384      |
| Simple / Reflector                  | `glm-5.3-flash` | Low      | 1.0         | 0.95  | 8192       |
| Searcher / Enricher / Simple JSON   | `glm-5.3-flash` | Low      | 1.0         | 0.95  | 4096       |

All default roles use Z.AI's recommended `temperature: 1.0` and `top_p: 0.95`. The full provider replay passed 288 of 289 cases; the sole failure was a transient gateway timeout and its focused retry passed.

**Thinking Modes**:
- **Always on** (GLM-5.3, GLM-5.3-Flash, GLM-5.3-FlashX): Cannot be disabled; `reasoning.effort` accepts `low`, `high`, or `max`
- **Hybrid** (GLM-5.2 and earlier GLM-5.x, GLM-4.7): Explicit toggle via `reasoning.mode`
- **Auto** (GLM-4.6, GLM-4.5 series): Model automatically determines when reasoning is needed
- **Preserved Thinking** (Z.AI Coding capability): all thinking-enabled agents in KeLai AGI also pass `extra_body.thinking.clear_thinking: false` so that `reasoning_content` from previous assistant turns is retained across the conversation. This is required on the standard API endpoint (`/api/paas/v4`) — on the Coding Plan endpoint it would be enabled by default. Improves reasoning continuity and cache hit rates in multi-turn tool call chains.
- All thinking-enabled agents also pass `extra_body.tool_choice: auto` defensively

**Key Features**:
- **Long-Horizon Tasks**: GLM-5.1 supports 8-hour sustained autonomous execution, ideal for complex multi-stage agentic workflows
- **OpenClaw-Native Orchestration**: GLM-5-Turbo is specifically optimized for tool invocation, instruction following, and long-chain execution
- **Prompt Caching**: Significant cost reduction on repeated context (cached input pricing shown)
- **Ultra-Long Context**: 200K tokens for GLM-5.x/4.7/4.6 series
- **MoE Architecture**: Efficient 744B/40B active (GLM-5/5.1), 355B/32B (GLM-4.5), 106B/12B (GLM-4.5-Air)
- **Tool Calling**: Seamless integration with the agents' tools via function calling
- **Streaming**: Real-time streaming with streaming tool calls support (GLM-4.6+)
- **Multilingual**: Exceptional Chinese and English NLP capabilities
- **Free Options**: GLM-4.7-Flash and GLM-4.5-Flash for prototyping and experimentation

**LiteLLM Integration**: Set `GLM_PROVIDER=zai` to enable model name prefixing when using default KeLai AGI configurations with LiteLLM proxy. Leave empty for direct API usage.

### Kimi Provider Configuration

KeLai AGI integrates with Kimi from Moonshot AI, providing ultra-long context models with multimodal capabilities perfect for analyzing extensive codebases and documentation.

#### Configuration Variables

| Variable           | Default Value                | Description                                         |
| ------------------ | -----------------------------| --------------------------------------------------- |
| `KIMI_API_KEY`     |                              | Kimi API key for authentication                     |
| `KIMI_SERVER_URL`  | `https://api.moonshot.ai/v1` | Kimi API endpoint URL (international)               |
| `KIMI_PROVIDER`    |                              | Provider prefix for LiteLLM integration (optional)  |

#### Configuration Examples

```bash
# Direct API usage (international endpoint)
KIMI_API_KEY=your_kimi_api_key
KIMI_SERVER_URL=https://api.moonshot.ai/v1

# Alternative endpoint
KIMI_SERVER_URL=https://api.moonshot.cn/v1  # China

# With LiteLLM proxy
KIMI_API_KEY=your_litellm_key
KIMI_SERVER_URL=http://litellm-proxy:4000
KIMI_PROVIDER=moonshot  # Adds prefix to model names (moonshot/kimi-k3) for LiteLLM
```

#### Supported Models

KeLai AGI supports the four current Kimi models with tool calling, streaming, context caching, and multimodal input. Kimi K2.5, the older K2 series, `kimi-latest`, `kimi-thinking-preview`, and all Moonshot V1 models are retired and not included. Models marked with `*` are used in the default configuration.

**Kimi K3 - Flagship (Always Reasoning)**

| Model ID   | Thinking | Multimodal | Context | Price (Input Miss / Output / Cache Hit) | Use Case                                                |
| ---------- | -------- | ---------- | ------- | --------------------------------------- | ------------------------------------------------------- |
| `kimi-k3`* | ✅ always | ✅          | 1M      | $3.00 / $15.00 / $0.30                  | Flagship for long-horizon coding and knowledge work; used by adviser with `reasoning_effort: max` |

**Kimi K2.7 Code Series - Coding-Focused**

| Model ID                    | Thinking | Multimodal | Context | Price (Input Miss / Output / Cache Hit) | Use Case                                                |
| ---------------------------- | -------- | ---------- | ------- | --------------------------------------- | ------------------------------------------------------- |
| `kimi-k2.7-code`*            | ✅ always | ✅          | 256K    | $0.95 / $4.00 / $0.19                   | Coding-focused model used by generator, refiner, coder, and pentester |
| `kimi-k2.7-code-highspeed`*  | ✅ always | ✅          | 256K    | $1.90 / $8.00 / $0.38                   | Same model as `kimi-k2.7-code` with higher output throughput (~180-260 tokens/s) (primary_agent/assistant default) |

**Kimi K2.6 - Multimodal Model**

| Model ID         | Thinking | Multimodal | Context | Price (Input Miss / Output / Cache Hit) | Use Case                                                |
| ---------------- | -------- | ---------- | ------- | --------------------------------------- | ------------------------------------------------------- |
| `kimi-k2.6`*     | ✅ hybrid | ✅          | 256K    | $0.95 / $4.00 / $0.16                   | Utility and installer model with optional thinking |

**Prices**: Per 1M tokens. K3 cache writes cost $3.00 with the default five-minute TTL or $6.00 with a one-hour TTL; cache hits cost $0.30. K2.7 and K2.6 table prices distinguish cache misses from cache hits.

> **CRITICAL — Kimi parameter constraints per model family**: API returns `invalid_request_error` for any deviation:
> - `kimi-k3`: always reasons, no `thinking` param at all; reasoning depth is set via the top-level `reasoning_effort` field (`low`/`high`/`max`, default `max`) — KeLai AGI pins it to `max` for all agents using this model. `temperature` MUST be `1.0`, `top_p` MUST be `0.95`, `n` MUST be `1`, `presence_penalty`/`frequency_penalty` MUST be `0`. Do not switch effort per call — it invalidates the prefix cache.
> - `kimi-k2.7-code` / `kimi-k2.7-code-highspeed`: `thinking` may be omitted; if set explicitly, only `{"type":"enabled","keep":"all"}` is accepted (`type: disabled` is rejected). `reasoning_effort` is not supported. `temperature` MUST be `1.0`, `top_p` MUST be `0.95`, `n` MUST be `1`; `tool_choice: required` is not supported (use `auto`).
> - `kimi-k2.6`: thinking mode needs `temperature=1.0`, `top_p=0.95`, `n=1`, `thinking.keep="all"`; non-thinking mode needs `temperature=0.6`, `top_p=0.95`, `n=1`.
> - All Kimi models: `presence_penalty=0`, `frequency_penalty=0`, `tool_choice` in `{auto, none}`.

**Default Agent Configuration**:

Strategy: `kimi-k2.6` handles utility and installer work, `kimi-k2.7-code-highspeed` serves the primary/assistant loop, `kimi-k2.7-code` serves tool-heavy generator/refiner/coder/pentester roles, and `kimi-k3` handles adviser reasoning. This configuration passed all 294 conformance cases.

| Agent Role                                   | Default Model               | Thinking              | Temperature | Top P | Max Output |
| -------------------------------------------- | --------------------------- | --------------------- | ----------- | ----- | ---------- |
| Adviser (mentor/planner)                     | `kimi-k3`                   | Always (effort=max)   | 1.0         | 0.95  | 8192       |
| Generator / Refiner                          | `kimi-k2.7-code`            | Always (keep=all)     | 1.0         | 0.95  | 32768      |
| Coder                                        | `kimi-k2.7-code`            | Enabled (keep=all)    | 1.0         | 0.95  | 20480      |
| Researcher                                   | `kimi-k2.7-code`            | Enabled (keep=all)    | 1.0         | 0.95  | 16384      |
| Primary Agent / Assistant                    | `kimi-k2.7-code-highspeed`  | Enabled (keep=all)    | 1.0         | 0.95  | 16384      |
| Installer                                    | `kimi-k2.6`                 | Enabled               | 1.0         | 0.95  | 16384      |
| Reflector / Searcher / Enricher              | `kimi-k2.6`                 | Disabled              | 0.6         | 0.95  | 4096       |
| Simple                                       | `kimi-k2.6`                 | Disabled              | 0.6         | 0.95  | 8192       |
| Simple JSON                                  | `kimi-k2.6`                 | Disabled              | 0.6         | 0.95  | 4096       |

**Key Features**:
- **Always-On Reasoning Flagship**: `kimi-k3` never disables thinking and offers a 1M token context for the most demanding long-horizon coding and knowledge work
- **Ultra-Long Context**: Up to 256K tokens (K2.7/K2.x) or 1M tokens (K3) for comprehensive codebase/documentation analysis
- **Native Multimodal**: K3, K2.7, and K2.6 support text, image, and video input
- **Hybrid Thinking**: K2.6 can toggle between thinking and non-thinking via `reasoning.mode`; K3 and K2.7 always reason
- **Preserved Thinking** (K2.7, K2.6): `thinking.keep: "all"` preserves historical `reasoning_content` across turns — required for multi-turn tool call chains
- **Automatic Context Caching**: K3/K2.7/K2.x models cache repeated prefixes
- **Tool Calling**: Full function-calling support for K3, K2.7, K2.x, and Moonshot V1
- **Coding-Optimized Variants**: `kimi-k2.7-code`/`kimi-k2.7-code-highspeed` target higher success rates on long-context programming tasks, with the highspeed variant tuned for throughput
- **Multilingual**: Strong Chinese, English, and multi-language support

**Multi-turn with thinking + tool calls**: KeLai AGI's universal reasoning preservation pattern (`TextPartWithReasoning` + `WithPreserveReasoningContent`) automatically ensures `reasoning_content` is sent back in the required TextContent → ToolCall order, satisfying Moonshot's "thinking is enabled but reasoning_content is missing in assistant tool call message" requirement.

**LiteLLM Integration**: Set `KIMI_PROVIDER=moonshot` to enable model name prefixing when using default KeLai AGI configurations with LiteLLM proxy. Leave empty for direct API usage.

### Qwen Provider Configuration

KeLai AGI uses one OpenAI-compatible door for both Qwen Cloud and Alibaba Cloud Model Studio (DashScope). The bundled catalogue is the union of their agent-capable Chat Completions models, while the default role configuration uses only model IDs available on both platforms.

#### Configuration Variables

| Variable           | Default Value                                          | Description                                         |
| ------------------ | ------------------------------------------------------ | --------------------------------------------------- |
| `QWEN_API_KEY`     |                                                        | Qwen Cloud, DashScope, or gateway API key           |
| `QWEN_SERVER_URL`  | `https://dashscope-us.aliyuncs.com/compatible-mode/v1` | Direct API or OpenAI-compatible gateway base URL    |
| `QWEN_PROVIDER`    |                                                        | Gateway route prefix: `qwen_cloud` or `dashscope`   |

#### Configuration Examples

```bash
# Direct Alibaba Cloud Model Studio usage (Global/US endpoint)
QWEN_API_KEY=your_qwen_api_key
QWEN_SERVER_URL=https://dashscope-us.aliyuncs.com/compatible-mode/v1
QWEN_PROVIDER=

# Alternative endpoints
QWEN_SERVER_URL=https://dashscope-intl.aliyuncs.com/compatible-mode/v1  # International (Singapore)
QWEN_SERVER_URL=https://dashscope.aliyuncs.com/compatible-mode/v1       # Chinese Mainland (Beijing)

# Qwen Cloud through LiteLLM
QWEN_API_KEY=your_litellm_key
QWEN_SERVER_URL=http://litellm-proxy:4000
QWEN_PROVIDER=qwen_cloud

# Alibaba Cloud through LiteLLM
QWEN_PROVIDER=dashscope
```

#### Supported Models

KeLai AGI ships 50 text, reasoning, coding, and vision-language catalogue entries available from at least one of the two platforms. Image generation, video, speech, realtime, translation-only, embedding, reranking, and decision APIs use different protocols or billing units and are not exposed as KeLai AGI chat models. Prices below are Alibaba Cloud international list rates per 1M tokens where Alibaba offers the model; Qwen Cloud can bill differently.

**Cross-platform model IDs used for portable configuration**

| Availability | Models |
| ------------ | ------ |
| Qwen Cloud and DashScope | `qwen3.8-max`, `qwen3.8-flash`, `qwen3.7-plus`, `qwen3.7-max`, `qwen3.6-flash`, `deepseek-v4.1-flash`, `deepseek-v4-pro` |
| Qwen Cloud only | `deepseek-v4-pro-0813` |
| DashScope only | All other entries in the bundled catalogue |

**Flagship Models (Top-tier Reasoning)**

| Model ID                     | Thinking | Intl | Global/US | China | Price (Input/Output/Cache) | Use Case                                                |
| ---------------------------- | -------- | ---- | --------- | ----- | -------------------------- | ------------------------------------------------------- |
| `qwen3.8-max`                | ✅        | —    | —         | —     | $2.00/$6.00/$0.25          | Newest flagship succeeding Qwen3.7-Max                  |
| `qwen3.8-omni-flash`         | ✅        | ✅    | ✅         | ✅     | $0.15/$0.47/—              | Current multimodal Omni tier                            |
| `qwen3.7-max`                | ✅        | ✅    | ✅         | ✅     | $2.50/$7.50/$0.50          | Next-gen flagship for agent-centric era                 |
| `qwen3.6-max-preview`        | ✅        | ✅    | ✅         | ✅     | $1.30/$7.80/$0.13          | Preview Max with enhanced vibe coding & front-end skills |
| `qwen3-max`                  | ✅        | ✅    | ✅         | ✅     | $1.20/$6.00/$0.24          | Previous-gen flagship with agent programming upgrades   |
| `qwen-max`                   | ❌        | ✅    | ✅         | ✅     | $1.60/$6.40/—              | Legacy 32K text flagship                                |
| `qwen-plus`                  | ✅        | ✅    | ✅         | ✅     | $0.40/$4.00/$0.08          | Qwen3-backbone Plus with switchable thinking modes      |

**Balanced Plus Models (Mid-tier)**

| Model ID                     | Thinking | Intl | Global/US | China | Price (Input/Output/Cache) | Use Case                                                |
| ---------------------------- | -------- | ---- | --------- | ----- | -------------------------- | ------------------------------------------------------- |
| `qwen3.7-plus`               | ✅        | —    | —         | —     | $0.40/$1.60/$0.08          | Cost-efficient tier of the Qwen3.7 generation           |
| `qwen3.6-plus`               | ✅        | ✅    | ✅         | ✅     | $0.50/$3.00/$0.05          | Native VL Plus with agentic coding                      |
| `qwen3.5-plus`               | ✅        | ✅    | ✅         | ✅     | $0.40/$2.40/$0.04          | Previous-gen native VL with strong multimodal capabilities |

**Fast Flash Models (Cost-optimized)**

| Model ID                     | Thinking | Intl | Global/US | China | Price (Input/Output/Cache) | Use Case                                                |
| ---------------------------- | -------- | ---- | --------- | ----- | -------------------------- | ------------------------------------------------------- |
| `qwen3.8-flash`              | ✅        | —    | —         | —     | $0.15/$0.47/$0.016         | Cost-efficient tier of the Qwen3.8 generation           |
| `qwen3.7-flash`              | ✅        | —    | —         | —     | $0.03/$0.13/$0.02          | Low-latency tier of the Qwen3.7 generation              |
| `qwen3.6-flash`*             | ✅        | ✅    | ✅         | ✅     | $0.25/$1.50/$0.05          | Shared utility model                                    |
| `qwen3.5-flash`              | ✅        | ✅    | ✅         | ✅     | $0.10/$0.40/$0.01          | Ultra-fast lightweight                                  |
| `qwen-flash`                 | ✅        | ✅    | ✅         | ✅     | $0.05/$0.40/$0.01          | Qwen3-series Flash with 1M context, tiered pricing      |

**Code-Specialized Models**

| Model ID                     | Thinking | Intl | Global/US | China | Price (Input/Output/Cache) | Use Case                                                |
| ---------------------------- | -------- | ---- | --------- | ----- | -------------------------- | ------------------------------------------------------- |
| `qwen3-coder-plus`           | ❌        | ✅    | ✅         | ✅     | $1.00/$5.00/$0.20          | Strong coding agent with autonomous programming         |
| `qwen3-coder-flash`          | ❌        | ✅    | ✅         | ✅     | $0.30/$1.50/$0.06          | Fast code-gen with multi-turn tool stability            |
| `qwen3-coder-next`           | ❌        | ✅    | ✅         | ✅     | $0.30/$1.50/—              | Open-source code generation, SOTA at same scale         |

**Vision-Language Models (Browser & Screenshot Analysis)**

| Model ID                     | Thinking | Intl | Global/US | China | Price (Input/Output/Cache) | Use Case                                                |
| ---------------------------- | -------- | ---- | --------- | ----- | -------------------------- | ------------------------------------------------------- |
| `qwen3-vl-plus`              | ✅        | ✅    | ✅         | ✅     | $0.20/$1.60/$0.04          | VL with visual agent capabilities, ultra-long video understanding |
| `qwen3-vl-flash`             | ✅        | ✅    | ✅         | ✅     | $0.05/$0.40/$0.01          | Small VL with 2D/3D localization for browser triage     |
| `qvq-max`                    | ✅        | ✅    | ✅         | ✅     | $1.20/$4.80/—              | Visual reasoning with chain-of-thought                  |
| `qwen3-vl-235b-a22b-thinking` | ✅        | —    | —         | —     | $0.40/$4.00/—              | Open-source 235B MoE VL (~22B active) with reasoning    |
| `qwen3-vl-235b-a22b-instruct` | ❌        | —    | —         | —     | $0.40/$1.60/—              | Open-source 235B MoE VL (~22B active), instruction-following |
| `qwen3-vl-32b-instruct`      | ❌        | —    | —         | —     | $0.16/$0.64/—              | Open-source 32B dense VL, instruction-following         |
| `qwen3-vl-30b-a3b-thinking`  | ✅        | —    | —         | —     | $0.20/$2.40/—              | Open-source 30B MoE VL (~3B active) with reasoning      |
| `qwen3-vl-30b-a3b-instruct`  | ❌        | —    | —         | —     | $0.20/$0.80/—              | Open-source 30B MoE VL (~3B active), instruction-following |

**Open-Source Qwen3.8 Series**

| Model ID                     | Thinking | Intl | Global/US | China | Price (Input/Output/Cache) | Use Case                                                |
| ---------------------------- | -------- | ---- | --------- | ----- | -------------------------- | ------------------------------------------------------- |
| `qwen3.8-2.4t-a95b`          | ✅        | —    | —         | —     | $2.00/$6.00/—              | Open-source 3.8 flagship, always reasons                |
| `qwen3.8-27b`                | ✅        | —    | —         | —     | $0.50/$3.00/$0.10          | Dense open-weight 3.8 tier, hybrid thinking, 1M context |

**Open-Source Qwen3.6 Series**

| Model ID                     | Thinking | Intl | Global/US | China | Price (Input/Output/Cache) | Use Case                                                |
| ---------------------------- | -------- | ---- | --------- | ----- | -------------------------- | ------------------------------------------------------- |
| `qwen3.6-27b`                | ✅        | ✅    | ✅         | ✅     | $0.60/$3.60/—              | Native VL on hybrid architecture, on-premises ready     |
| `qwen3.6-35b-a3b`            | ✅        | ✅    | ✅         | ✅     | $0.25/$1.49/—              | Efficient 35B MoE (~3B active) for continuous monitoring |

**Open-Source Qwen3.5 Series**

| Model ID                     | Thinking | Intl | Global/US | China | Price (Input/Output/Cache) | Use Case                                                |
| ---------------------------- | -------- | ---- | --------- | ----- | -------------------------- | ------------------------------------------------------- |
| `qwen3.5-397b-a17b`          | ✅        | ✅    | ✅         | ✅     | $0.60/$3.60/—              | Largest 397B params (~17B active), exceptional reasoning |
| `qwen3.5-122b-a10b`          | ✅        | ✅    | ✅         | ✅     | $0.40/$3.20/—              | Large 122B params (~10B active), strong balance         |
| `qwen3.5-35b-a3b`            | ✅        | ✅    | ✅         | ✅     | $0.25/$2.00/—              | Efficient 35B MoE (~3B active), cost-effective          |
| `qwen3.5-27b`                | ✅        | ✅    | ✅         | ✅     | $0.30/$2.40/—              | Medium 27B with hybrid linear attention + sparse MoE    |

**Open-Source Qwen3 Coder Series**

| Model ID                              | Thinking | Intl | Global/US | China | Price (Input/Output/Cache) | Use Case                                                |
| ------------------------------------- | -------- | ---- | --------- | ----- | -------------------------- | ------------------------------------------------------- |
| `qwen3-coder-480b-a35b-instruct`      | ❌        | ✅    | ✅         | ✅     | $1.50/$7.50/—              | Largest open coder MoE (480B/~35B active)               |
| `qwen3-coder-30b-a3b-instruct`        | ❌        | ✅    | ✅         | ✅     | $0.45/$2.25/—              | Efficient 30B MoE (~3B active), repository-scale        |

**Open-Source Qwen3 Dense & MoE Series**

| Model ID                              | Thinking | Intl | Global/US | China | Price (Input/Output/Cache) | Use Case                                                |
| ------------------------------------- | -------- | ---- | --------- | ----- | -------------------------- | ------------------------------------------------------- |
| `qwen3-next-80b-a3b-thinking`         | ✅        | ✅    | ✅         | ✅     | $0.15/$1.20/—              | Next-gen 80B MoE (~3B active) thinking-only             |
| `qwen3-next-80b-a3b-instruct`         | ❌        | ✅    | ✅         | ✅     | $0.15/$1.20/—              | Next-gen 80B MoE instruction-following                  |
| `qwen3-235b-a22b-thinking-2507`       | ✅        | —    | —         | —     | $0.23/$2.30/—              | 235B MoE (~22B active) thinking variant                 |
| `qwen3-30b-a3b-thinking-2507`         | ✅        | —    | —         | —     | $0.20/$2.40/—              | 30B MoE (~3B active) thinking variant                   |
| `qwen3-30b-a3b-instruct-2507`         | ❌        | —    | —         | —     | $0.20/$0.80/—              | 30B MoE (~3B active) non-thinking variant               |

**Third-Party Models**

These models share the OpenAI-compatible Qwen door. Availability differs by platform; the cross-platform table above is authoritative for portable role configuration.

| Model ID                              | Thinking | Context | Price (Input/Output/Cache) | Use Case                                                |
| ------------------------------------- | -------- | ------- | -------------------------- | ------------------------------------------------------- |
| `glm-5.3-prime`                       | ✅        | 1M      | $2.80/$8.80/—              | DashScope priority GLM route |
| `glm-5.2-fast-preview`                | ✅        | 1M      | $2.80/$8.80/—              | DashScope high-throughput GLM 5.2 route |
| `glm-5.2`                             | ✅        | 1M      | $1.40/$4.40/$0.28          | Zhipu AI GLM flagship route |
| `deepseek-v4.1-flash`*                | ✅        | 1M      | $0.15/$0.60/—              | Current shared low-latency DeepSeek model |
| `deepseek-v4-pro`*                    | ✅        | 1M      | $2.40/$4.80/$0.20          | Shared model for reasoning- and tool-heavy roles |
| `deepseek-v4-pro-0813`                | ✅        | 1M      | $2.40/$4.80/$0.20          | Qwen Cloud-only V4 Pro snapshot |
| `deepseek-v4-flash-0731`              | ✅        | 1M      | $0.44/$1.32/$0.088         | Legacy DashScope Flash snapshot |
| `kimi-k3`                             | ✅        | 1M      | $3.00/$15.00/$0.30         | Current Kimi flagship on DashScope |
| `kimi-k2.7-code`                      | ✅        | 262K    | $0.95/$4.00/$0.19          | Moonshot coding model, thinking always on, 16K output ceiling |

**Prices**: Per 1M tokens. Cache pricing reflects implicit cache hit (when available); MoE/dense open-source models do not expose cache pricing. Tiered models (Max/Plus) show lowest-tier pricing (typically ≤32k or ≤256k input); larger contexts incur higher rates per Alibaba Cloud pricing. `deepseek-v4-flash-0731` shows the busy-hours rate; idle hours bill half.

**Region Availability**:
- **Intl** (International): Singapore region (`dashscope-intl.aliyuncs.com`)
- **Global/US**: US Virginia region (`dashscope-us.aliyuncs.com`)
- **China**: Chinese Mainland Beijing region (`dashscope.aliyuncs.com`)
- **—**: availability in that region is not recorded here

**Default Agent Configuration**:
| Agent Role                                                                   | Default Model       | Tier      |
| ---------------------------------------------------------------------------- | ------------------- | --------- |
| `simple`, `simple_json`, `reflector`, `searcher`, `enricher`                 | `qwen3.6-flash`     | Utility   |
| `primary_agent`, `assistant`, `coder`, `installer`, `pentester`              | `deepseek-v4.1-flash` | Workhorse |
| `generator`, `refiner`, `adviser`                                            | `deepseek-v4-pro`   | Planning  |

The role configuration must run unchanged behind `qwen_cloud/` and `dashscope/`. Qwen 3.6 Flash handles utility calls, DeepSeek V4.1 Flash provides the lower-cost workhorse tier, and DeepSeek V4 Pro remains on planning and advice. Thinking controls are left at provider defaults because Qwen Cloud does not accept the same disable operation that DashScope supports.

**Key Features**:
- **Agent-Centric Design**: Qwen3.7-Max is purpose-built for long-horizon autonomous execution and tool invocation
- **Automatic Context Caching**: 30-50% cost reduction on repeated context with implicit cache
- **Extended Thinking**: Chain-of-thought reasoning for complex analysis (Qwen3.7/3.6/3.5/3-Max, QVQ-Max)
- **Code Specialization**: Qwen3-Coder series with multi-turn tool interaction and repository-level understanding
- **Vision-Language**: Qwen3-VL series for browser screenshot triage, 2D/3D localization, OCR-level analysis
- **Tool Calling**: Seamless integration with the agents' tools via function calling
- **Streaming**: Real-time response streaming for interactive workflows
- **Multilingual**: Strong Chinese, English, and multi-language support
- **Open-Source Variants**: Dense and MoE models from 27B to 2.4T for on-premises/air-gapped deployments

**LiteLLM Integration**: Set `QWEN_PROVIDER=qwen_cloud` for Qwen Cloud routes or `QWEN_PROVIDER=dashscope` for Alibaba Cloud routes. Leave it empty for direct DashScope API usage.

#### Alternative Integrations

DashScope is fully OpenAI-compatible, so Qwen can also power two other KeLai AGI subsystems through the standard OpenAI client.

**As embedding provider** (`text-embedding-v4`, see [Alibaba Cloud Model Studio pricing](https://modelstudio.console.alibabacloud.com/ap-southeast-1?tab=doc#/doc/?type=model&url=prices)):

```bash
EMBEDDING_PROVIDER=openai
EMBEDDING_URL=https://dashscope-intl.aliyuncs.com/compatible-mode/v1  # International (Singapore)
# EMBEDDING_URL=https://dashscope.aliyuncs.com/compatible-mode/v1     # Chinese Mainland
EMBEDDING_KEY=sk-*******
EMBEDDING_MODEL=text-embedding-v4
EMBEDDING_BATCH_SIZE=         # optional, default applies
EMBEDDING_STRIP_NEW_LINES=    # optional, default applies
```

> Note: the Global/US DashScope endpoint (`dashscope-us.aliyuncs.com`) does **not** expose embedding APIs — use the International or China endpoints for `text-embedding-v4`.

**As OpenAI-typed custom LLM provider**: instead of the dedicated `QWEN_*` variables, you can wire any Qwen chat model through KeLai AGI's custom OpenAI-compatible provider by pointing `OPENAI_SERVER_URL` (or a custom provider entry) to the DashScope `/compatible-mode/v1` endpoint and selecting the desired Qwen model name. Useful when you already manage all model traffic through a single OpenAI-shaped client (e.g. shared with LiteLLM/OneAPI proxies).

### MiniMax Provider Configuration

KeLai AGI integrates with MiniMax's M-series through the OpenAI-compatible `https://api.minimax.io/v1` endpoint: large-context agentic models with tool calling, JSON output, and streaming.

#### Configuration Variables

| Variable             | Default Value               | Description                                        |
| -------------------- | --------------------------- | -------------------------------------------------- |
| `MINIMAX_API_KEY`    |                             | MiniMax API key for authentication                 |
| `MINIMAX_SERVER_URL` | `https://api.minimax.io/v1` | MiniMax API endpoint URL                           |
| `MINIMAX_PROVIDER`   |                             | Provider prefix for LiteLLM integration (optional) |

#### Configuration Examples

```bash
# Direct API usage
MINIMAX_API_KEY=your_minimax_api_key
MINIMAX_SERVER_URL=https://api.minimax.io/v1

# With LiteLLM proxy
MINIMAX_API_KEY=your_litellm_key
MINIMAX_SERVER_URL=http://litellm-proxy:4000
MINIMAX_PROVIDER=minimax  # Adds prefix to model names (minimax/MiniMax-M3) for LiteLLM
```

#### Supported Models

KeLai AGI ships 3 MiniMax models with tool calling, JSON output, and streaming. `MiniMax-M3` is the default catalogue model; the bundled agent configuration uses `MiniMax-M2.7` for roles where conformance testing showed more reliable context retention and unified-diff generation.

| Model ID                 | Context | Price (Input/Output, ≤512K context) | Use Case                                                                                             |
| ------------------------ | ------- | ------------------------------------ | --------------------------------------------------------------------------------------------------- |
| `MiniMax-M3`*            | ~1M     | $0.30/$1.20 (2x above 512K tokens)   | Latest flagship for agentic reasoning, tool use, code generation, and long-context tasks            |
| `MiniMax-M2.7`           | 204K    | $0.30/$1.20                          | Reliable reasoning and coding model used by stateful and diff-producing agents                       |
| `MiniMax-M2.7-highspeed` | 204K    | $0.60/$2.40                          | Low-latency variant of M2.7 for fast-response scenarios                                              |

**LiteLLM Integration**: Set `MINIMAX_PROVIDER=minimax` to enable model name prefixing when using default KeLai AGI configurations with LiteLLM proxy. Leave empty for direct API usage.

### Mistral Provider Configuration

KeLai AGI talks to Mistral through the OpenAI-compatible `https://api.mistral.ai/v1` endpoint: the
Medium/Large/Small line, the Ministral small models, Codestral for code, and one Z.ai model Mistral
serves itself.

#### Configuration Variables

| Variable             | Default Value               | Description                                        |
| -------------------- | --------------------------- | -------------------------------------------------- |
| `MISTRAL_API_KEY`    |                             | Mistral API key for authentication                 |
| `MISTRAL_SERVER_URL` | `https://api.mistral.ai/v1` | Mistral API endpoint URL                           |
| `MISTRAL_PROVIDER`   |                             | Provider prefix for LiteLLM integration (optional) |

#### Configuration Examples

```bash
# Direct API usage
MISTRAL_API_KEY=your_mistral_api_key
MISTRAL_SERVER_URL=https://api.mistral.ai/v1

# With LiteLLM proxy
MISTRAL_API_KEY=your_litellm_key
MISTRAL_SERVER_URL=http://litellm-proxy:4000
MISTRAL_PROVIDER=mistral  # Adds prefix to model names (mistral/mistral-small-latest) for LiteLLM
```

#### Supported Models

KeLai AGI ships 6 Mistral models. The default configuration uses `mistral-small-latest` for utility and read-heavy agents, `mistral-large-latest` for primary and execution agents, and `mistral-medium-latest` for planning and advice. Small, Medium, and Large each have a 256K-token context window.

| Model ID                | Price (Input/Output) | Reasons | Use Case                                                                 |
| ----------------------- | -------------------- | ------- | ------------------------------------------------------------------------ |
| `mistral-small-latest`* | $0.15/$0.60          | yes     | Hybrid instruct/reasoning/coding model for utility and read-heavy agents |
| `mistral-medium-latest` | $1.50/$7.50          | yes     | Frontier-class multimodal model for planning and advice                  |
| `mistral-large-latest`  | $0.50/$1.50          | no      | General-purpose model for primary, assistant, coding, and research       |
| `codestral-latest`      | $0.30/$0.90          | no      | Code completion and fill-in-the-middle                                   |
| `zai-glm-5-2`           | $1.40/$4.40          | yes     | Z.ai GLM 5.2 served by Mistral, public preview                           |
| `zai-glm-5-3`           | $1.40/$4.40          | yes     | Z.ai GLM 5.3 served by Mistral, public preview; thinking cannot be turned off |

**LiteLLM Integration**: Set `MISTRAL_PROVIDER=mistral` to enable model name prefixing when using default KeLai AGI configurations with LiteLLM proxy. Leave empty for direct API usage.

### xAI (Grok) Provider Configuration

KeLai AGI talks to xAI through the OpenAI-compatible `https://api.x.ai/v1` endpoint: the Grok 4.x line with large context windows, plus a coding model.

#### Configuration Variables

| Variable         | Default Value           | Description                                        |
| ---------------- | ----------------------- | -------------------------------------------------- |
| `XAI_API_KEY`    |                         | xAI API key for authentication                     |
| `XAI_SERVER_URL` | `https://api.x.ai/v1`   | xAI API endpoint URL                               |
| `XAI_PROVIDER`   |                         | Provider prefix for LiteLLM integration (optional) |

#### Configuration Examples

```bash
# Direct API usage
XAI_API_KEY=your_xai_api_key
XAI_SERVER_URL=https://api.x.ai/v1

# With LiteLLM proxy
XAI_API_KEY=your_litellm_key
XAI_SERVER_URL=http://litellm-proxy:4000
XAI_PROVIDER=xai  # Adds prefix to model names (xai/grok-4.3) for LiteLLM
```

#### Supported Models

KeLai AGI lists seven xAI Chat Completions model IDs that responded through the tested API. Catalogue availability does not imply suitability as an agent default. The bundled configuration uses `grok-4.3` for tool-heavy roles, including `searcher`, and `grok-4.20-0309-non-reasoning` for `simple`, `simple_json`, `reflector`, and `enricher`; the exact bindings are in `backend/pkg/providers/xai/config.yml`. In saved generator chains, `grok-4.3` called `subtask_list` directly on all nine, whereas `grok-4.6` returned text without a tool call on all nine. The Responses-only multi-agent model is not listed because KeLai AGI agents supply their own client-side tools.

| Model ID | Context | Price (Input/Output) | Reasons | Saved-chain result |
| --- | --- | --- | --- | --- |
| `grok-4.7` | 500K | $2.00/$6.00 | yes | API responds; generator/reflector refusals make it unsuitable as a default |
| `grok-4.6` | 500K | $2.00/$6.00 | yes | API responds; all nine saved generator requests returned text without a tool |
| `grok-4.5` | 500K | $2.00/$6.00 | yes | API responds; eight of nine generator requests explicitly refused |
| `grok-4.3`* | 1M | $1.25/$2.50 | yes | Direct plan on all nine saved generator chains; tool-role default and fallback |
| `grok-4.20-0309-reasoning` | 1M | $1.25/$2.50 | yes | Four direct plans, three more after safe tool continuation, two pending |
| `grok-4.20-0309-non-reasoning`* | 1M | $1.25/$2.50 | no | Utility-role default; six plans after safe tool continuation, three pending |
| `grok-build-0.1` | 256K | $1.00/$2.00 | yes | Coding model; five plans after safe tool continuation, four pending |

> Note: prices are for prompts under 200K tokens; at or above that threshold xAI applies the higher long-context rate to every token in the request.

**LiteLLM Integration**: Set `XAI_PROVIDER=xai` to enable model name prefixing when using default KeLai AGI configurations with LiteLLM proxy. Leave empty for direct API usage.
