# Multi-step / multi-agent coding pipelines vs. a single agent with a direct prompt (same model)

Research date: 2026-11-20. Conventions used below:
- "Measured" = the source reports a number from its own experiment. "Vendor claim" = self-reported by a company with a product interest. "Snippet only" = seen in a search-result summary, not confirmed by opening the primary page.
- Results from 2023 to mid-2024 (GPT-3.5/GPT-4-era) are flagged as possibly outdated: several sources below state that the benefit of structure shrinks as models improve.

## 1. What do controlled comparisons show for multi-agent / staged frameworks vs. single-agent baselines with the same model?

### Takeaway
The controlled same-model evidence from 2025-2026 mostly shows ties or losses for multi-agent setups on coding-style tasks: on SWE-bench Verified every multi-agent topology tested by Google Research scored below the single agent (-2.1% to -14.9%), and a single agent simulating a homogeneous multi-agent workflow matched it on HumanEval/MBPP at equal or lower cost. Gains appear mainly on decomposable, parallel tasks and when the single-agent baseline is weak (below roughly 45% success).

### Cited Findings

**Google Research / MIT, "Towards a Science of Scaling Agent Systems" (arXiv 2512.08296; v1 Dec 2025, v3 Apr 2026)**
- Scope (v3 abstract): 260 configurations, six agentic benchmarks, five architectures (single agent, independent, centralized, decentralized, hybrid); effects range from "+80.8% on decomposable financial reasoning to -70.0% on sequential planning"; a predictive model picks the best architecture for 87% of held-out configurations; cross-validated R^2 = 0.373 (0.413 with a task-grounded capability metric). — [arXiv abstract](https://arxiv.org/abs/2512.08296)
- The Google blog (Jan 28, 2026) describes an earlier version: 180 configurations, 4 benchmarks (Finance-Agent, BrowseComp-Plus, PlanCraft, Workbench), 3 model families (GPT, Gemini, Claude), R^2 = 0.513. The numbers differ from the v3 abstract because the paper was extended. — [Google Research blog](https://research.google/blog/towards-a-science-of-scaling-agent-systems-when-and-why-agent-systems-work/)
- SWE-bench Verified (added in the later version), single-agent mean 0.522: Hybrid 0.511 (-2.1%), Centralized 0.506 (-3.1%), Decentralized 0.494 (-5.4%), Independent 0.444 (-14.9%). All multi-agent variants underperformed the single agent. — [arXiv HTML v3](https://arxiv.org/html/2512.08296v3)
- Capability saturation: above a single-agent baseline of about 45% (raw threshold ~0.45) the paper reports negative returns from adding agents (beta = -0.236, p = 0.004). — [arXiv HTML v3](https://arxiv.org/html/2512.08296v3)
- Overall mean multi-agent improvement over single agent: -0.3% (95% CI [-58.7%, +77.2%], SD 37.5%), i.e. no average gain but very high variance by task. — [arXiv HTML v3](https://arxiv.org/html/2512.08296v3)
- Sequential planning (PlanCraft): every multi-agent variant degraded performance by 39-70%. Parallelizable Finance-Agent: centralized coordination +80.9%. — [Google Research blog](https://research.google/blog/towards-a-science-of-scaling-agent-systems-when-and-why-agent-systems-work/)
- Trace-level error amplification vs. single agent (1.0): centralized 4.4x, hybrid 5.1x, decentralized 7.8x, independent 17.2x. The paper notes these terms were not statistically significant in its regression. — [arXiv HTML v3](https://arxiv.org/html/2512.08296v3)

**"Rethinking the Value of Multi-Agent Workflow: A Strong Single Agent Baseline" (arXiv 2601.12307, Jan 18, 2026)**
- Claim: when all agents share one base model (homogeneous workflow), a single agent using multi-turn conversation reaches the same performance and is cheaper through KV-cache reuse; it cannot emulate truly heterogeneous (different-model) workflows. — [arXiv abstract](https://arxiv.org/abs/2601.12307)
- Measured (GPT-4o mini, mean of 3 runs), same workflow run as multi-agent vs. as single agent: AFlow HumanEval 90.1 vs 91.1, MBPP 78.8 vs 78.8; OneFlow HumanEval 91.6 vs 92.1, MBPP 81.1 vs 81.4. Plain IO prompt baseline: HumanEval 89.1, MBPP 72.6. — [arXiv HTML](https://arxiv.org/html/2601.12307)
- Mixed result the report writer should not hide: single-agent AFlow drops on HotpotQA (68.4 vs 72.1 multi-agent) and multi-agent AFlow leads slightly on GSM8K/MATH/DROP. — [arXiv HTML](https://arxiv.org/html/2601.12307)
- Cost (USD, GPT-4o mini, per benchmark run): AFlow multi-agent vs single-agent MBPP 0.393 vs 0.283, HotpotQA 1.438 vs 0.530, DROP 0.771 vs 0.345; cost figures use a simulated "ideal" KV-cache estimate because the model is closed-weight. — [arXiv HTML](https://arxiv.org/html/2601.12307)
- Caveat: the benchmarks are function-level (HumanEval, MBPP), not repository-level software engineering.

**"Single-agent or Multi-agent Systems? Why Not Both?" (arXiv 2505.18286, May 23, 2025, UIUC)**
- Abstract: "the benefits of MAS over SAS diminish as LLM capabilities improve"; frontier models (o3, Gemini 2.5 Pro) reduced the limitations that motivated multi-agent designs; a hybrid cascade that routes requests between single- and multi-agent "improves accuracy by 1.1-12%" and cuts deployment cost by up to 20%. — [arXiv abstract](https://arxiv.org/abs/2505.18286v1)

**"Why Do Multi-Agent LLM Systems Fail?" / MAST (arXiv 2503.13657, v3 Oct 2025, NeurIPS 2025 D&B)**
- Dataset: 1,642 annotated traces across 7 frameworks (ChatDev, MetaGPT, HyperAgent, AppWorld, AG2, Magentic-One, OpenManus); 14 failure modes in 3 categories; human inter-annotator kappa 0.88; LLM annotator (o1 few-shot) accuracy 0.94, kappa 0.77. — [arXiv HTML v3](https://arxiv.org/html/2503.13657v3)
- Failure rates across the open-source systems studied range from 41% to 86.7%. — [arXiv HTML v3](https://arxiv.org/html/2503.13657v3)
- The paper states that multi-agent "performance gains often remain minimal compared to single-agent frameworks" (qualitative statement; the paper gives no single-agent numeric baseline in the text read). — [arXiv HTML v3](https://arxiv.org/html/2503.13657v3)
- Failure-mode shares: step repetition 15.7%, reasoning-action mismatch 13.2%, unaware of termination conditions 12.4%, disobey task specification 11.8%, incorrect verification 9.1%, no/incomplete verification 8.2%, task derailment 7.4%, fail to ask for clarification 6.8%, premature termination 6.2%, loss of conversation history 2.8%, conversation reset 2.2%, ignored other agent's input 1.9%, disobey role specification 1.5%, information withholding 0.85%. — [arXiv HTML v3](https://arxiv.org/html/2503.13657v3)
- Category totals (summed from the mode shares by the fetch tool, not quoted from the paper): system design ~44.2%, inter-agent misalignment ~32.4%, task verification ~23.5%. — [arXiv HTML v3](https://arxiv.org/html/2503.13657v3)
- MetaGPT vs ChatDev with the same model: MetaGPT has 60-68% fewer system-design and inter-agent-misalignment failures but 1.56x more task-verification failures. — [arXiv HTML v3](https://arxiv.org/html/2503.13657v3)
- Interventions on ChatDev (GPT-4o): better role specification / hierarchy gave +9.4% task success; adding high-level task-objective verification gave +15.6% on ProgramDev. Failures remained substantial after both. — [arXiv HTML v3](https://arxiv.org/html/2503.13657v3)

**Multi-agent debate vs. single-agent sampling (older, non-coding; possibly outdated)**
- "Large Language Models Cannot Self-Correct Reasoning Yet" (2023): with an equal number of responses, multi-agent debate underperforms simple self-consistency majority voting on GSM8K. Snippet only. — [arXiv 2310.01798](https://arxiv.org/pdf/2310.01798)
- "Should we be going MAD?" (2023): multi-agent debate systems "do not reliably outperform other proposed prompting strategies, such as self-consistency". Snippet only. — [arXiv 2311.17371](https://arxiv.org/html/2311.17371v3)
- "Talk Isn't Always Cheap" (2025): debate can degrade accuracy below a single agent acting alone. Snippet only. — [arXiv 2509.05396](https://arxiv.org/html/2509.05396v1)

**Original framework claims (2023, GPT-3.5/GPT-4 era; outdated baseline)**
- MetaGPT (Aug 2023) claims SOP-encoded role pipelines produce "more coherent solutions than previous chat-based multi-agent systems"; the comparison in the abstract is against other multi-agent systems, not against a strong single agent. — [arXiv 2308.00352](https://arxiv.org/abs/2308.00352)

**Agentless (arXiv 2407.01489, 2024; outdated models)**
- A fixed non-agentic pipeline (localize, repair, validate) reached 32.00% on SWE-bench Lite (96 fixes) at $0.70 per issue, the best among open-source agents at the time; an earlier version reported 27.33% at $0.34 with GPT-4o. Snippet only for exact numbers. — [arXiv 2407.01489](https://arxiv.org/abs/2407.01489)

### Inferences
- The strongest same-model, coding-relevant data point is the Google SWE-bench Verified result: structure in the form of multiple coordinating agents did not help and mildly hurt when the single agent already solves about half the tasks.
- MAST shows that staged role pipelines (MetaGPT-style SOPs) trade one failure class for another: fewer coordination failures, more verification failures. A pipeline's review stage is therefore not automatically a safety net.
- Agentless is evidence that a fixed staged pipeline could beat free-form agents in 2024, when models were weak at long tool loops. It does not contradict the 2025-2026 findings; it fits the "structure helps weaker models" pattern.
- The 2601.12307 result implies that with one base model, role separation by itself adds no capability; whatever gain a workflow gives comes from the steps performed, not from splitting them across agents.

### Gaps
- I found no peer-reviewed, same-model study that directly compares a spec, plan, code, review pipeline with a single direct-prompt agent on repository-level tasks (SWE-bench Verified/Pro scale) with a 2026 frontier model. The Google paper tests coordination topologies, not staged SDLC pipelines.
- Per-framework failure rates in MAST (e.g. ChatDev vs MetaGPT individually) are in figures I could not read; only the 41%-86.7% range is confirmed.
- A widely repeated blog figure of "72.2% multi-agent vs ~65% single agent on SWE-bench Verified" could not be traced to a primary source; do not use.
- A CircleCI blog claiming a team configuration "improved 75% more than the solo agent" with the same model was seen only as a search snippet and is not a controlled study. — [CircleCI blog](https://loop.circleci.com/one-agent-vs-a-team-what-benchmark-data-says-about-multi-agent-debugging)
- The "More Agents Is All You Need" and Self-MoA papers were not retrieved.

## 2. On SWE-bench Verified / SWE-bench Pro / Terminal-Bench leaderboards, how do minimal scaffolds compare with elaborate ones using the same model, and how has the gap changed?

### Takeaway
With the same model, minimal scaffolds (mini-SWE-agent, Terminus 2) land within a few points of elaborate product harnesses, sometimes above and sometimes below; the Terminal-Bench 2.0 authors conclude model choice usually matters more than scaffold. Product harnesses still show a measurable edge in some pairings (about 0-9 points), so the gap has narrowed but not vanished.

### Cited Findings

**mini-SWE-agent / SWE-bench bash-only**
- mini-SWE-agent README: agent class is "some 100 lines of python", "Does not have any tools other than bash", "completely linear history", and "Scores >74% on the SWE-bench verified benchmark". — [GitHub README](https://github.com/SWE-agent/mini-swe-agent)
- The same README states the rationale: in 2024 SWE-agent "placed a lot of emphasis on tools and special interfaces for the agent", but "as LMs have become more capable, a lot of this is not needed at all to build a useful agent". — [GitHub README](https://github.com/SWE-agent/mini-swe-agent)
- SWE-bench's bash-only leaderboard runs every model through mini-SWE-agent to isolate model from scaffold; it describes results as "the state-of-the-art LM performance when given just a bash shell and a problem". Releases 1.x and 2.x are not directly comparable (2.x uses tool calling). — [SWE-bench Verified page](https://swebench.com/verified)
- Snippet only: one aggregator snapshot lists Claude 4.5 Opus (high) at 76.80% on mini-SWE-agent 2.0.0. A third-party archive (vals.ai) lists much higher figures for later models under mini-SWE-agent (up to 97.00%); I could not verify these against the official leaderboard. — [vals.ai archive](https://www.vals.ai/benchmarks/swebench)

**Terminal-Bench 2.0 paper (arXiv 2601.11868, Jan 2026), same model across harnesses (measured, Appendix Table 2)**
- Claude Opus 4.5: Terminus 2 57.8%, Claude Code 52.1%, OpenHands 51.9% (the minimal neutral harness wins).
- Claude Sonnet 4.5: Terminus 2 42.8%, mini-SWE-agent 42.5%, OpenHands 40.3%, Claude Code 40.1% (a spread of 2.7 points).
- Claude Opus 4.1: Terminus 2 38.0%, mini-SWE-agent 35.1%, OpenHands 34.9%, Claude Code 34.8%.
- Claude Haiku 4.5: mini-SWE-agent 29.8%, Terminus 2 28.3%, Claude Code 27.5%, OpenHands 13.3%.
- GPT-5.2: Codex CLI 62.9%, Terminus 2 54.0% (product harness +8.9).
- GPT-5: Codex CLI 49.6%, OpenHands 41.5%, Terminus 2 35.2%, mini-SWE-agent 33.9% (product harness +14 to +16).
- Gemini 2.5 Pro: Terminus 2 32.6%, mini-SWE-agent 26.1%, Gemini CLI 19.6%, OpenHands 15.7% (minimal harness beats the vendor CLI by 13 points).
- Author conclusion: "model selection is usually more important than agent scaffold when optimizing for performance."
- The paper reports essentially no correlation between turns per trial and success, and that higher token counts do not necessarily mean better performance; token use for one benchmark pass ranged from 3.9M input tokens (Opus 4.5 + Terminus 2) to 663.1M (Haiku 4.5 + OpenHands).
- All of the above — [Terminal-Bench 2.0 paper](https://arxiv.org/html/2601.11868v1)

**Terminal-Bench 2.1 leaderboard (snippet only; the official page did not render when fetched)**
- Reported same-model pairs: Claude Opus 4.8 Claude Code 78.9% vs Terminus 2 74.6%; Claude Opus 4.7 Claude Code 69.7% vs Terminus 2 66.1%; Gemini 3.1 Pro Gemini CLI 70.7% vs Terminus 2 70.3%; GPT-5.5 Terminus 2 78.2% (a mirror lists Codex CLI at about 83%). Margins of error about ±2.4 to ±2.9. — [tbench.ai leaderboard](https://www.tbench.ai/leaderboard/terminal-bench/2.1)

**SWE-bench Pro (vendor report, Augment, Feb 2026)**
- Same model (Claude Opus 4.5), 731 public tasks: Auggie CLI 51.80%, Cursor 50.21%, Claude Code 49.75%; Scale's standardized SWE-Agent baseline with the same model 45.89%. Codex with GPT-5.2-codex 46.47% (different model). Augment ran its own evaluations; the SWE-Agent number comes from Scale's leaderboard, so run conditions are not identical. — [Augment blog](https://www.augmentcode.com/blog/auggie-tops-swe-bench-pro)

### Inferences
- Spread between harnesses for one model is typically 2-9 points in these tables, with outliers both ways (GPT-5 +16 for Codex CLI; Gemini 2.5 Pro -13 for Gemini CLI). Product harnesses are single-agent loops with better tools and context retrieval, not role pipelines, so their edge is evidence for tool/context quality rather than for staged multi-agent structure.
- The trend across model generations is not monotonic: for Claude models in the 2.0 paper the minimal harness is equal or better; in the 2.1 snippet data product harnesses lead by about 4 points. Any claim that "scaffolds no longer matter" overstates the data; "scaffold matters less than model, and simple loops are competitive" is supported.
- The SWE-bench Pro vendor table suggests retrieval/context quality yields about 4-6 points over a generic scaffold at the same model. This is self-reported and within plausible run-to-run noise for the top three entries (1.6-2.0 points apart).

### Gaps
- Current official top-of-board numbers for the SWE-bench bash-only leaderboard could not be read (page returned 404 or no table rows).
- Terminal-Bench 2.1 rows are from a search summary only; verify on the live page before quoting.
- No leaderboard entry was found that isolates a multi-agent or staged pipeline against a single-agent loop with the same model and matched budget. Blog claims of "22+ point scaffold swings on SWE-bench Pro" were unsourced and are excluded.
- Benchmark tasks are short-to-medium, well-specified, and auto-graded; they under-represent ambiguous requirements, where a spec stage might matter. No source measured that directly.

## 3. What do vendor engineering posts say?

### Takeaway
Vendors converge on "start with the simplest single agent and add structure only when measured results justify it"; the only vendor-reported large multi-agent win is for breadth-first research, not coding, and it costs about 15x chat tokens. By 2026 both Cognition and Anthropic describe narrow patterns that do help coding: read-only subagents, a clean-context reviewer, and planner/evaluator roles for work beyond what the model does reliably alone.

### Cited Findings

**Anthropic, "Building effective agents" (Dec 19, 2024)**
- "we recommend finding the simplest solution possible, and only increasing complexity when needed"; "you should consider adding complexity only when it demonstrably improves outcomes." — [Anthropic](https://www.anthropic.com/engineering/building-effective-agents)
- "Agentic systems often trade latency and cost for better task performance". — [Anthropic](https://www.anthropic.com/engineering/building-effective-agents)
- Frameworks "often create extra layers of abstraction that can obscure the underlying prompts and responses". — [Anthropic](https://www.anthropic.com/engineering/building-effective-agents)
- Prompt chaining "is ideal for situations where the task can be easily and cleanly decomposed into fixed subtasks"; orchestrator-workers suit "complex tasks where you can't predict the subtasks needed"; evaluator-optimizer is "particularly effective when we have clear evaluation criteria". — [Anthropic](https://www.anthropic.com/engineering/building-effective-agents)
- On their SWE-bench agent: "we actually spent more time optimizing our tools than the overall prompt." — [Anthropic](https://www.anthropic.com/engineering/building-effective-agents)

**Anthropic, "How we built our multi-agent research system" (Jun 13, 2025)**
- Opus 4 lead with Sonnet 4 subagents "outperformed single-agent Claude Opus 4 by 90.2% on our internal research eval" (internal eval, research tasks, and a heterogeneous model mix, so not a pure same-model comparison). — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system)
- "token usage by itself explains 80% of the variance"; three factors (tokens, tool calls, model choice) "explained 95% of the performance variance" on BrowseComp. — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system)
- "agents typically use about 4× more tokens than chat interactions, and multi-agent systems use about 15× more tokens than chats"; viability requires "tasks where the value of the task is high enough to pay for the increased performance." — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system)
- Poor fit: domains "that require all agents to share the same context"; "most coding tasks involve fewer truly parallelizable tasks than research"; agents "are not yet great at coordinating and delegating to other agents in real time." — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system)

**Anthropic, "Harness design for long-running apps" (Mar 24, 2026)**
- Solo agent vs. planner/generator/evaluator harness on a full app build with Opus 4.5: solo 20 minutes and $9; harness 6 hours and $200, "over 20x more expensive, but the difference in output quality was immediately apparent" (the solo build's central feature did not work). Single-case demonstration, not a benchmark. — [Anthropic](https://www.anthropic.com/engineering/harness-design-long-running-apps)
- With Opus 4.6 the harness was simplified: sprint decomposition removed because the model "could natively handle the job without this sort of decomposition"; context resets were already dropped with Opus 4.5; the evaluator moved to a single pass at the end. The planner was kept because the generator under-scoped without it. Total for a later build: about 3 h 50 min and $124.70. — [Anthropic](https://www.anthropic.com/engineering/harness-design-long-running-apps)
- "every component in a harness encodes an assumption about what the model can't do on its own", and such assumptions "can quickly go stale as models improve." — [Anthropic](https://www.anthropic.com/engineering/harness-design-long-running-apps)
- The evaluator "is worth the cost when the task sits beyond what the current model does reliably solo"; for tasks within the model's reliable range it "became unnecessary overhead." — [Anthropic](https://www.anthropic.com/engineering/harness-design-long-running-apps)

**Cognition, "Don't Build Multi-Agents" (Walden Yan, Jun 12, 2025)**
- Principles: "Share context, and share full agent traces, not just individual messages" and "Actions carry implicit decisions, and conflicting decisions carry bad results". — [Cognition](https://cognition.com/blog/dont-build-multi-agents)
- "The simplest way to follow the principles is to just use a single-threaded linear agent"; "in 2025, running multiple agents in collaboration only results in fragile systems." — [Cognition](https://cognition.com/blog/dont-build-multi-agents)
- On the 2024 split between a large planning model and a small edit-apply model: the small model "would misinterpret the instructions of the large model"; "Today, the edit decision-making and applying are more often done by a single model in one action." — [Cognition](https://cognition.com/blog/dont-build-multi-agents)
- On Claude Code subagents: "the subtask agent is usually only tasked with answering a question, not writing any code", and its benefit is keeping investigative work out of the main agent's history. — [Cognition](https://cognition.com/blog/dont-build-multi-agents)

**Cognition, "Multi-Agents: What's Actually Working" (Walden Yan, Apr 22, 2026)**
- Working patterns: read-only subagents (which "mostly resemble tool calls rather than true multi-agent collaboration"); a generator-verifier loop where "the coding and review agents do not share any context beforehand"; a smaller primary model calling a larger one as a tool; manager/child agents in a map-reduce-and-manage shape. — [Cognition](https://cognition.com/blog/multi-agents-working)
- Vendor claim: the review agent catches an average of 2 bugs per PR, about 58% classed as severe. No single-agent self-review control is reported. — [Cognition](https://cognition.com/blog/multi-agents-working)
- Still not working: parallel-writer swarms, which "still don't see meaningful adoption"; unstructured agent networks are called "mostly a distraction". In the model-mix pattern "the quality ceiling was set by the primary". — [Cognition](https://cognition.com/blog/multi-agents-working)

**LangChain and OpenAI**
- LangChain's "How and when to build multi-agent systems" reconciles the Cognition and Anthropic posts around context engineering and read-heavy vs. write-heavy tasks. Snippet only. — [LangChain blog](https://blog.langchain.com/how-and-when-to-build-multi-agent-systems)
- OpenAI's "A practical guide to building agents" advises starting with a single agent and moving to multi-agent only when complexity demands it. Secondary summaries only; the primary PDF was not opened. — [ibl.ai summary](https://ibl.ai/blog/openai-a-practical-guide-to-building-agents)

### Inferences
- The "bitter lesson" argument applied to harnesses is stated most concretely by Anthropic's March 2026 post (each harness component is a stale-prone assumption about model weakness) and the mini-SWE-agent README (special interfaces from 2024 are no longer needed). Both come with evidence of components being deleted after a model upgrade.
- Vendor positions moved between 2025 and 2026 from "avoid multi-agent" to "a few disciplined patterns work". The surviving patterns share one property: only one agent writes, and other agents either read or judge with a fresh context.
- A separate reviewer with a clean context is the one staged element with repeated vendor support for coding. A separate planner is supported only for long builds where the generator under-scopes; a mandatory spec stage for small tasks has no vendor support in the sources read.

### Gaps
- No Google DeepMind engineering post on this question was retrieved (only the Google Research paper).
- The OpenAI guide's exact wording is unverified.
- I did not find a primary essay titled as a "bitter lesson for agent harnesses"; the argument appears in the sources above without that label.
- Vendor posts are self-reported and mostly anecdotal; the $9 vs $200 comparison is a single run.

## 4. Under which conditions does extra structure help, and under which does it hurt?

### Takeaway
Structure helps when work is parallel and read-heavy, exceeds one context window, or sits beyond what the model does reliably alone (including weaker models); it hurts on sequential, tightly coupled tasks where splitting fragments context and lets errors compound, and it stops paying once the single-agent baseline is already high.

### Cited Findings

**Helps**
- Parallelizable, decomposable tasks: +80.9% with centralized coordination on Finance-Agent. — [Google Research blog](https://research.google/blog/towards-a-science-of-scaling-agent-systems-when-and-why-agent-systems-work/)
- "heavy parallelization, information that exceeds single context windows, and interfacing with numerous complex tools" are the stated sweet spots (quoted via search summary of the post). — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system)
- Very large tasks: a single linear agent runs into "issues for very large tasks with so many subparts that context windows start to overflow"; Cognition's answer is history compression, not multiple writers. — [Cognition](https://cognition.com/blog/dont-build-multi-agents)
- Tasks beyond the model's solo reliability: the evaluator "continued to give real lift" at the edge of the generator's ability. — [Anthropic](https://www.anthropic.com/engineering/harness-design-long-running-apps)
- Weak single-agent baseline: positive returns to coordination are concentrated below about 45% single-agent success. — [arXiv 2512.08296 v3](https://arxiv.org/html/2512.08296v3)
- Clean-context review: a reviewer that shares no context with the coder avoids accumulated context noise. — [Cognition](https://cognition.com/blog/multi-agents-working)
- Heterogeneous models: a single model cannot emulate a different-model workflow; however the paper's own heterogeneous pilot stayed bounded by the best homogeneous workflow. — [arXiv 2601.12307](https://arxiv.org/html/2601.12307)
- Verifiable intermediate artifacts: MetaGPT-style SOPs cut system-design and misalignment failures by 60-68% relative to ChatDev. — [MAST paper](https://arxiv.org/html/2503.13657v3)

**Hurts**
- Sequential reasoning: every multi-agent variant lost 39-70% on PlanCraft. — [Google Research blog](https://research.google/blog/towards-a-science-of-scaling-agent-systems-when-and-why-agent-systems-work/)
- Strong baseline on coding: all multi-agent variants below single agent on SWE-bench Verified (-2.1% to -14.9%). — [arXiv 2512.08296 v3](https://arxiv.org/html/2512.08296v3)
- Tool-heavy tasks: negative efficiency-tools interaction (beta = -0.096, p = 0.002); the blog gives a coding agent with 16+ tools as an example. — [arXiv 2512.08296 v3](https://arxiv.org/html/2512.08296v3)
- Context fragmentation and conflicting implicit decisions (the split Flappy Bird example: mismatched background and bird). — [Cognition](https://cognition.com/blog/dont-build-multi-agents)
- Hand-off misreading between stages: a downstream model "would misinterpret the instructions" of the upstream one. — [Cognition](https://cognition.com/blog/dont-build-multi-agents)
- Compounding errors without central verification: independent agents amplify errors 17.2x vs 4.4x centralized. — [arXiv 2512.08296 v3](https://arxiv.org/html/2512.08296v3)
- Weak verification stages: incorrect verification 9.1% and no/incomplete verification 8.2% of observed failures; MetaGPT shows 1.56x more verification failures than ChatDev. — [MAST paper](https://arxiv.org/html/2503.13657v3)
- "Overthinking" where downstream agents receive redundant information, with simpler single-agent systems sometimes winning. Snippet only. — [arXiv 2505.18286](https://arxiv.org/pdf/2505.18286)
- Shared-context requirement and interdependent subtasks, typical of coding. — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system)

### Inferences
- A spec, plan, code, review pipeline is sequential by construction, so it sits on the side where the Google data shows losses, unless each stage adds verification the single agent would not perform. The evidence favors keeping one writer with full context and adding an independent check, over passing summaries between role agents.
- The "helps weaker models" condition means the value of a fixed pipeline should fall with each model generation; Anthropic's removal of sprints and context resets between Opus 4.5 and 4.6 is a concrete instance.
- Hand-offs through written artifacts (spec, plan) are a lossy channel by Cognition's first principle; the loss is smaller when the next stage can also read the full prior trace.

### Gaps
- No source quantifies the effect of an explicit requirements/spec stage on ambiguous real-world tasks; benchmark tasks arrive pre-specified.
- No source measured human-in-the-loop gates (approval between stages), which may be the main purpose of a staged workflow and is orthogonal to raw resolve rate.
- The ~45% saturation threshold comes from one regression over six benchmarks; it should be treated as indicative, not as a law.

## 5. What cost multipliers (tokens, wall time) are reported?

### Takeaway
Reported multipliers range from about 1.6x to more than 20x a single agent: 58%-515% token overhead by topology in the Google study, about 7x for agent teams in plan mode, about 15x chat for multi-agent research, and 20x cost with 18x wall time for a planner/generator/evaluator build.

### Cited Findings
- Token overhead vs. single agent, with success rate and success per 1K tokens: single 0% / 0.466 / 67.7; independent 58% / 0.370 / 42.4; decentralized 263% / 0.477 / 23.9; centralized 285% / 0.463 / 21.5; hybrid 515% / 0.452 / 13.6. Turns per task: 7.2 single vs 11.4-44.3 multi-agent. — [arXiv 2512.08296 v3](https://arxiv.org/html/2512.08296v3)
- The same paper says configurations were "matched for total reasoning-token budget (mean 4,800 tokens per trial)" and elsewhere that multi-agent variants used roughly 1.6-6.2x the single agent's tokens at matched performance; the two statements are not fully consistent as extracted. — [arXiv 2512.08296 v3](https://arxiv.org/html/2512.08296v3)
- Agents about 4x chat tokens; multi-agent systems about 15x chat tokens (so roughly 3.75x a single agent, by division). — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system)
- Parallel subagents and parallel tool calls "cut research time by up to 90% for complex queries" (wall time falls while tokens rise). — [Anthropic](https://www.anthropic.com/engineering/multi-agent-research-system)
- "Agent teams use approximately 7x more tokens than standard sessions when teammates run in plan mode"; token use is "roughly proportional to team size". — [Claude Code docs](https://code.claude.com/docs/en/costs)
- Planner/generator/evaluator vs. solo (Opus 4.5): $200 and 6 h vs $9 and 20 min. In the later Opus 4.6 run, QA rounds cost $3.09-$4.06 each and the planner $0.46, against $113.85 for the three build rounds, so review and planning were under 10% of the $124.70 total. — [Anthropic](https://www.anthropic.com/engineering/harness-design-long-running-apps)
- Same workflow run as separate agents vs. one agent (GPT-4o mini): 1.4x-2.7x more expensive as multi-agent on MBPP, DROP and HotpotQA (0.393 vs 0.283; 0.771 vs 0.345; 1.438 vs 0.530), equal on HumanEval. — [arXiv 2601.12307](https://arxiv.org/html/2601.12307)
- Hybrid routing between single- and multi-agent reduces deployment cost by up to 20%. — [arXiv 2505.18286](https://arxiv.org/abs/2505.18286v1)
- Harness choice alone changes token use by orders of magnitude with the same benchmark: 3.9M to 663.1M input tokens per pass in the Terminal-Bench 2.0 table (different models, so not a clean multiplier). — [Terminal-Bench 2.0 paper](https://arxiv.org/html/2601.11868v1)

### Inferences
- In the Anthropic build example, most of the 20x cost came from longer generation, not from the planner or evaluator stages, which were cheap. A single end-of-run review is a low-cost addition; repeated build/QA rounds are the expensive part.
- Since Anthropic attributes 80% of research-eval variance to tokens spent, part of any multi-agent gain may be a budget effect. A fair pipeline-vs-single comparison needs matched token budgets, which most vendor reports lack.
- For sequential coding work, parallel wall-time savings do not apply, so a staged pipeline pays the token overhead and also adds latency.

### Gaps
- No source reports a token or wall-time multiplier specifically for a spec, plan, code, review pipeline on SWE-bench-class tasks.
- Cost figures come from different models, price points and dates and are not comparable across sources.
- The "3.75x a single agent" figure is my arithmetic from Anthropic's two ratios, not a number Anthropic states.
