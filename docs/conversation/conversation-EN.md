# AI Agent Architecture Design Conversation Record (English Version)

## 1. Initial Evaluation Request

**User**: First evaluate the feasibility of the following AI agent object abstraction:
    Purpose: Currently, Kubernetes lacks a core abstraction for AI Agent. The goal is to define a core resource similar to Pod that can abstract any Agent framework, any form of Agent, whether it's LangChain, ADK, OpenClaw, Hermes Agent, etc. At the same time, externalize various scaffolding capabilities of the Agent, such as CLI Tools, MCP, Skills, Knowledge, RAG, Memory, State, Guardrail, Security, Policy, Gateway, Sandbox, etc. These external capabilities can be connected through the AI Agent ID. Note that for an AI Agent, it can be in the same place as the Sandbox execution environment, or it can be separated from the Sandbox execution environment. For example, several AI Agents correspond to the same Sandbox execution environment, but during actual execution, the Sandbox only executes one AI Agent's logic. That is, AI Agent and Sandbox have an N:M correspondence. An Agent framework process can correspond to only one AI Agent resource ID, or it can correspond to multiple AI Agent resource IDs. Overall, the AI Agent needs to be sufficiently flexible in abstraction, achieving a high degree of abstraction capability similar to Pod or process for unknown business scenarios.
    Solution approach: Upgrade or secondarily encapsulate Pod. A Pod contains an Agent Handler process. This Agent Handler has its own image and is responsible for monitoring the AI Agent resource IDs that the image is responsible for. Then, inside the Pod, based on the AI Agent's framework choice, start the corresponding process (container), such as ADK or OpenClaw process (container). In ADK/OpenClaw, through configuration or other methods, create the corresponding AI Agent and associate it with the AI Agent resource ID. That is, ultimately the AI Agent ID is associated with the Agent framework process. Multiple AI Agent IDs may be associated with the same process ID, or one AI Agent ID may be associated with one Agent ID. This is determined by the Agent Handler's code logic.

**Coding Agent**: I will evaluate the feasibility of this AI Agent abstraction solution from multiple dimensions.

Overall Evaluation: The solution approach is **basically feasible**, but there are several key issues that need to be re-examined.

### Solution Advantages
- Unified Abstraction Layer: Using CRD to define AI Agent resources can align with the K8s ecosystem
- Framework Independent: Agent Handler as sidecar/initializer mode can indeed decouple framework differences
- Externalized Capabilities Modularization: Making MCP/Skills/RAG etc. as independent components, connected through Agent ID, follows microservice decoupling principles
- N:M Mapping Flexibility: Agent and Sandbox decoupling supports resource pooling

### Key Issues
1. Pod secondary encapsulation positioning is unclear
2. Agent Handler's role boundary is blurred
3. N:M mapping runtime conflicts
4. External capability binding mechanism is missing
5. Process and Agent ID mapping logic is missing

### Core Recommendations
1. Use CRD to define AI Agent as a first-class resource, not Pod secondary encapsulation
2. Move Agent Handler up to Operator layer
3. Design CapabilityBinding mechanism for external capabilities
4. First implement 1:1 isolation model, then extend to N:M shared model

Do you need me to further refine the design of any specific part?

---

## 2. One-by-One Discussion Request

**User**: Let's discuss each question or decision point one by one separately

**Coding Agent**: Okay, let's discuss one by one.

### Question 1: Pod Secondary Encapsulation Positioning

You mentioned "upgrade or secondary encapsulate Pod", there are two completely different technical paths here:

**Path A: CRD + Operator Mode**
- AI Agent is an independent first-class resource, similar to Deployment, StatefulSet
- Pod is just the runtime carrier of AI Agent
- Operator responsible for creating, monitoring, rebuilding Pod

**Path B: Pod Embedded Handler Mode**
- Agent Handler as the "super process" inside Pod
- Manages framework container lifecycle inside Pod

My judgment: Path A (CRD + Operator) better aligns with K8s design philosophy.

What's your thought? Do you want to go with Path A or Path B?

---

## 3. Handler Responsibility Clarification

**User**: There's an issue here. If we adopt the CRD controller approach, it will lead to confusion in development responsibilities. The Agent framework is decided by actual business personnel, so managing and starting the Agent, i.e., knowing what input information the specific framework should handle, such as how to provide prompts, how to provide tools, etc., can only be concretely handled by the chosen Agent framework and the handler corresponding to that Agent framework. Adopting CRD controller means each Agent framework needs an independent controller. But for the platform layer, it would prefer a unified abstraction that can abstract any Agent framework type, such as OpenClaw and CrewAI, ADK are completely different types of Agent frameworks. If we use the controller solution, it would be one controller per Agent framework. But using the Agent Handler approach, the corresponding Agent framework can provide a handler, while the AI Agent CRD resource's controller can be uniformly abstracted as one type.

**Coding Agent**: I understand, this is a good distinction.

Your core point:

| Layer | Responsibility | Who's Responsible |
|-------|----------------|-------------------|
| Controller | Platform layer, unified abstraction, manages AIAgent CRD lifecycle | Platform team |
| Agent Handler | Framework layer, concrete handling of prompt/tools/framework startup | Framework provider/Business developers |

This way, the Controller doesn't need to know the difference between ADK and CrewAI, it just needs to know "which Handler to call".

Confirm understanding: So the architecture becomes Controller creating Pod and injecting Handler image, Handler provided by framework, knows how to start framework process and configure prompt/tools.

Is your understanding correct?

---

## 4. Handler and Framework Process Relationship

**User**: The understanding is correct, more inclined to Option A (Handler as sidecar, framework process is independent container), but for container isolation, can we reduce the heaviness and minimize container isolation overhead as much as possible

**Coding Agent**: Okay, this requirement is practical.

### Container Isolation Overhead Sources
- Image size: Each container needs independent image
- Startup time: Container startup takes a few seconds
- Memory usage: Each container has independent process space
- Network stack: Inter-container communication needs to go through localhost

### Lightweight Solution Comparison

**Solution 1: Shared Process Namespace (Recommended)**
- Two containers share PID namespace, can see each other's processes
- Still maintains file system, network namespace isolation
- Handler can monitor framework process through /proc, even send signals
- Overhead: Almost no extra overhead, just a K8s configuration

**Solution 2: Single Container Multiple Processes**
- Only one container, most lightweight
- Problem: Handler and framework strongly coupled, image needs customization

What level of isolation granularity do you want?

---

## 5. Namespace Sharing Decision

**User**: Shared process namespace, and for network namespace, there's no need for isolation, but file system needs isolation. Because Agent Handler and the framework process running the actual AI Agent are seen as tightly coupled inter-process relationships, but only for the need of independent image release, can be independently released and upgraded, but there's no special security isolation requirement. Considering that each AI Agent and framework process may be one-to-one or many-to-one relationship, therefore each AI Agent needs independent file space

**Coding Agent**: I understand, summarizing the constraints:

| Dimension | Choice |
|-----------|--------|
| Process Namespace | Shared |
| Network Namespace | Shared |
| File System | Isolated |
| Image | Independent release |

Need to clarify: You mentioned "each AI Agent needs independent file space". Here assuming a scenario where one framework process corresponds to 3 AI Agent IDs (many-to-one). What does "independent file space" refer to?

**Option A: Isolate directories by AI Agent ID**
**Option B: Isolate Volume/PVC by AI Agent ID**

---

## 6. PVC Granularity Decision

**User**: Is Option B more flexible and convenient for data backup, recovery, and management?

**Coding Agent**: Yes, Option B's advantages:

| Dimension | Advantage |
|-----------|-----------|
| Backup Recovery | Independent backup by Agent granularity, no mutual impact |
| Scaling Migration | Agent-1 data too large, can independently migrate to larger storage |
| Lifecycle | After Agent deletion, PVC can choose to retain or cascade delete |
| Permission Control | Different Agent's PVCs can set different StorageClass/encryption policies |

Confirm solution: Each AI Agent CRD created, Controller automatically creates corresponding PVC.

Next question: When AI Agent is deleted, how to handle PVC?

**Option A: Cascade delete**
**Option B: Retain PVC**
**Option C: User specifies policy in CRD**

---

## 7. PVC Lifecycle Policy Decision

**User**: User choice

**Coding Agent**: Okay, confirmed.

Confirmed solutions:

| Dimension | Decision |
|-----------|----------|
| Process Namespace | Shared |
| Network Namespace | Shared |
| File System | Independent PVC by Agent ID |
| PVC Lifecycle | User specifies policy in CRD |

Next question: Handler and Framework process communication

Since sharing network namespace, Handler and framework process can communicate through localhost. Need to confirm communication direction.

---

## 8. Communication Method Decision

**User**: This design itself wants to let the Agent Handler decide what method to use to manage Agent and Agent framework in a Pod, so is it better to leave this to the Agent Handler? But provide any possibility to Agent Handler and Agent framework

**Coding Agent**: I understand. Platform layer only provides infrastructure capabilities, specific communication method is decided by Handler and framework.

Confirmed: Platform layer doesn't enforce communication protocol.

Next question: Handler image source

Since Handler is responsible for "knowing how to start and manage framework", who provides the Handler image?

**Option A: Framework community provides**
**Option B: Platform layer provides generic Handler template**
**Option C: Both coexist**

---

## 9. Handler Image Source Decision

**User**: A Framework community provides, if community doesn't provide, naturally the business personnel using this framework will develop it themselves

**Coding Agent**: Okay, confirmed.

Handler image is provided by framework ecosystem, platform layer makes no assumptions.

Next question: How to specify Handler in CRD

When user creates AI Agent, how to tell the system which Handler to use?

**Option A: Directly specify Handler image**
**Option B: Specify framework type, Handler auto-inferred**
**Option C: Hybrid mode**

---

## 10. Handler Specification Method Decision

**User**: Option A

**Coding Agent**: Okay, confirmed.

User directly specifies Handler image and Framework image, platform layer doesn't infer.

Next question: Agent ID generation

When creating AIAgent CRD, how to determine Agent ID?

**Option A: CRD name is Agent ID**
**Option B: Auto-generate UUID**
**Option C: User specified or auto-generated**

---

## 11. Agent ID Design Discussion

**User**: Both name and uuid are needed. name is more suitable for human reading, but uniqueness should be determined by uuid. For example, under the same namespace, different Agent frameworks might be used but with the same name, they can still be uniquely distinguished through uuid. Of course, we can also decide that name is unique, determined by the person or tool creating the CR. Which do you recommend?

**Coding Agent**: I recommend name unique.

Reasons:
- K8s convention: Users are familiar
- kubectl experience: Directly use name
- Complexity: Simple, no extra fields needed

Supplement: K8s auto-generates metadata.uid for each resource. If absolutely unique identifier needed, can use metadata.uid as Agent ID.

Do you think this is acceptable?

---

## 12. Agent ID Confirmation

**User**: Okay, handle according to K8s convention

**Coding Agent**: Okay, confirmed.

Confirmed:
- metadata.name: User specified, unique within namespace, human readable
- metadata.uid: K8s auto-generated, absolutely unique identifier

Handler decides which to use as Agent ID.

Next question: Many-to-one mapping

You mentioned "multiple AI Agent IDs can correspond to one framework process". Who decides to merge multiple Agents into one process?

**Option A: User explicitly specifies in CRD**
**Option B: Handler automatically decides**
**Option C: Platform layer doesn't support, implemented inside Handler**

---

## 13. Many-to-One Mapping Decision (Triggering Architecture Refactoring)

**User**: The most critical issue is actually how to associate multiple AI Agents to one Agent Handler instance, i.e., one Pod. Any suggestions on this? Specifically, whether Handler has one framework process per Agent ID or one framework process corresponding to multiple Agent IDs, is decided by Agent Handler.

**Coding Agent**: This is a core architecture issue. Let me outline several solutions:

**Solution A: AgentGroup CRD (Explicit grouping)**
**Solution B: Selector mode (Label binding)**
**Solution C: Specify target Pod in AIAgent**
**Solution D: Handler image as grouping key**

What's your preference?

---

## 14. Architecture Refactoring Proposal

**User**: Should the relationship be like this: Agent Handler and Agent Framework as a merged object, Agent Handler and Pod are one-to-one correspondence, and AI Agent is another object that can be mapped or scheduled to the merged object of Agent Handler and Agent Framework. Through the decoupling of AI Agent and the merged object, AI Agent can dynamically schedule or migrate to different merged objects (underneath is Pod).

**Coding Agent**: I understand, this思路 is clearer.

New architecture understanding:

Analogy:
- AgentRuntime similar to Node (runtime carrier)
- AIAgent similar to Pod (schedulable workload)

Next question: Who is responsible for scheduling?

**Option A: Platform Controller scheduling**
**Option B: User specifies Runtime**
**Option C: Hybrid mode**

---

## 15. Scheduling Mode Decision

**User**: Should be hybrid mode, i.e., if AI Agent specifies runtime type, Controller auto-schedules; if specifies runtime instance, then fixed binds to that runtime instance

**Coding Agent**: Okay, confirmed. This aligns with K8s Node scheduling mode.

Confirmed scheduling mode:
- runtimeRef.type: Auto schedule
- runtimeRef.name: Fixed binding

Next question: Runtime migration

You mentioned AIAgent can dynamically migrate to different Runtime. This involves several details.

**Option A: User actively triggers**
**Option B: Controller auto migrates**
**Option C: Both supported**

---

## 16. Migration Support Decision

**User**: Both supported

**Coding Agent**: Okay, confirmed.

Next question: Data/state handling during migration

During migration process, how to handle AIAgent's PVC, Memory, runtime state?

**Option A: PVC follows migration**
**Option B: Data copy migration**
**Option C: State rebuild**

---

## 17. PVC Migration Decision

**User**: Option A: PVC follows migration

**Coding Agent**: Okay, confirmed.

During migration, PVC follows AIAgent, unbinds from old Pod and mounts to new Pod.

Technical constraint reminder: PVC cross-node migration requires storage backend support (network storage like NFS, Ceph, Longhorn).

Does this match your expectation?

---

## 18. Storage Constraint Confirmation

**User**: Matches expectation

**Coding Agent**: Okay, continue to next question.

Next question: External capability binding

You initially mentioned external capabilities (MCP, Skills, RAG, Memory, Guardrail, etc.) are connected through Agent ID.

Need to confirm: Bind external capabilities to AIAgent or AgentRuntime?

---

## 19. External Capability Binding Discussion

**Coding Agent**: First need to catalog all external capabilities involved by Agent, which ones are Agent-specific needing association with Agent ID, i.e., each AI Agent corresponds to different content; which ones are for the same Agent Runtime, all AI Agents see the same capability.

Capability list:
- MCP: Tool/capability integration
- Skills: Skill modules
- CLI Tools: Command line tools
- Knowledge/RAG: Knowledge base, retrieval augmented generation
- Memory: Conversation history, long-term memory, context
- State: Runtime state, task progress
- Guardrail: Output constraints, content filtering, safety guardrail
- Security: Security policy, authentication authorization
- Policy: Policy control, compliance constraints
- Gateway: API gateway, traffic entry
- Sandbox: Execution isolation environment

Please confirm/supplement: Is the above list complete? Which are Agent-level? Which are Runtime-level?

---

## 20. Capability Binding Mode

**User**: Should be like this: for all AI Agents using the same Agent Runtime, the public configuration capability is the same, but each AI Agent can have its own configuration, such as allowing what capability, prohibiting what capability. If AI Agent doesn't customize, it's the same. If customizes, then apply the AI Agent's customization onto the public capability to form its customized capability.

**Coding Agent**: I understand, this is an inheritance + override mode.

Next question: Capability configuration priority

If AIAgent customized configuration conflicts with Runtime public configuration, how to handle?

**Option A: AIAgent priority**
**Option B: Runtime priority**
**Option C: Policy control**

---

## 21. Configuration Priority Decision

**User**: If configuration conflicts, as long as the capability is obtainable and implementable, AI Agent takes precedence

**Coding Agent**: Okay, confirmed.

Confirmed configuration priority: AI Agent customized configuration > Runtime public configuration (premise: capability is obtainable and implementable).

Next question: Capability validation

When AIAgent customized configuration requests a capability, how to validate its availability?

**Option A: Controller validates at creation time**
**Option B: Handler validates at runtime**
**Option C: Both combined**

---

## 22. Capability Validation Decision

**User**: Option A

**Coding Agent**: Okay, confirmed.

Controller validates capability availability when creating AIAgent, rejects creation if unavailable.

Next question: Capability resource management itself

How to define capabilities like MCP, Memory, RAG, Sandbox?

**Option A: Each capability independent CRD**
**Option B: Unified Capability CRD**
**Option C: Capability as embedded configuration in Runtime/AIAgent**

---

## 23. Capability CRD Design Discussion

**User**: Agent Handler's significance is to support different Agent frameworks through the same pattern, so should we standardize various capability definitions in Agent Runtime CRD?

**Coding Agent**: Good point. This way Handler's core value can be manifested.

Handler reads standardized configuration, converts to format needed by respective frameworks.

Next question: Capability definition sharing

If capability definition is embedded in Runtime CRD, how to handle when multiple Runtimes need to use the same MCP instance?

**Option A: Each Runtime independently defines configuration**
**Option B: Reference external capability instance**

---

## 24. Capability Reference Method Discussion

**User**: Considering AI Agent may also customize capabilities, so should it be an independent but more suitable than standardized capability set CRD, this way both Agent Handler and Agent itself can reference

**Coding Agent**: I understand, this design is more reasonable.

Confirmed: Capability as independent CRD.

Next question: Capability CRD scope

**Option A: Namespace level**
**Option B: Cluster level**

---

## 25. Capability Scope Discussion

**User**: Considering multi-tenancy, namespace level is more appropriate, but cluster level usage mode is not excluded either

**Coding Agent**: Okay, this aligns with K8s Role/ClusterRole mode.

Confirmed: Two-level Capability (Capability and ClusterCapability).

Next question: Reference rules

**Option A: Auto lookup**
**Option B: Explicitly specify level**

---

## 26. Terminology Adjustment and Scope Decision

**User**: Don't use the word "capability" to represent Agent's various capabilities, use "harness" to represent various scaffolding of AI Agent/Agent Handler. Lookup method adopts K8s convention

**Coding Agent**: Okay, switch to Harness.

CRD naming adjustment:
- Harness (Namespace level)
- ClusterHarness (Cluster level)

What "K8s convention" specifically refers to?

---

## 27. Remove Cluster-Level Harness

**User**: Let's remove cluster sharing mode, consider multi-tenancy

**Coding Agent**: Okay, confirmed. Simplify design, only keep Namespace-level Harness.

In multi-tenancy scenarios, each Namespace independently manages its Harness resources.

Next question: Harness type standardization

Need to confirm what standard types exist.

---

## 28. Harness Type List Confirmation

**User**: Later there may be types needing extension, such as adding structured output, first proceed with currently known ones

**Coding Agent**: Okay, confirmed.

Currently supported types (extensible later): mcp, skills, cli-tools, knowledge, memory, state, guardrail, security, policy, gateway, sandbox.

Next question: Harness CRD spec structure

**Option A: Unified spec, distinguish by type**
**Option B: Each type independent spec field**
**Option C: Hybrid mode**

---

## 29. Harness Spec Structure Decision

**User**: Option B: Each type independent spec field

**Coding Agent**: Okay, confirmed.

Next question: Multi-type support

Can one Harness CRD simultaneously configure multiple types?

**Option A: Single type**
**Option B: Multi-type combination**

---

## 30. Single Type Decision

**User**: Option A: Single type

**Coding Agent**: Okay, confirmed.

One Harness CRD only configures single type, identified by type field.

Next question: AgentRuntime and Harness binding relationship

**Option A: Reference Harness name list**
**Option B: Reference by type grouping**

---

## 31. Harness Reference Method Decision

**User**: Option B: Reference by type grouping

**Coding Agent**: Okay, confirmed.

Next question: AIAgent customization override

How to express specifically?

**Option A: Reference + configuration override**
**Option B: allow/deny mode**

---

## 32. AIAgent Customization Expression Decision

**User**: Option A: Reference + configuration override

**Coding Agent**: Okay, confirmed.

Next question: AIAgent append extra Harness

Can AIAgent reference extra Harness not in AgentRuntime?

**Option A: Allow append**
**Option B: Don't allow append**

---

## 33. No Append Decision

**User**: Option B: Don't allow append

**Coding Agent**: Okay, confirmed.

AIAgent can only override or deny Harness already in AgentRuntime, cannot append new Harness.

Next question: How Handler gets Harness information

Handler at startup needs to know which Harness the current Runtime is associated with.

**Option A: Handler queries CRD through API**
**Option B: Controller injects configuration**
**Option C: Hybrid mode**

---

## 34. Configuration Delivery Method Discussion

**User**: How does current sidecar handle this kind of configuration? Can we reference sidecar's handling method?

**Coding Agent**: Let me outline K8s common sidecar configuration handling methods:

Sidecar configuration handling modes:
- Istio Envoy: xDS API dynamic push
- Dapr: HTTP API get configuration
- Fluent Bit: ConfigMap mount
- Vault Agent: API dynamic get

Three typical modes:
- Mode A: Shared Volume mount
- Mode B: Dynamic API push
- Mode C: Handler actively queries

Which do you prefer?

---

## 35. Shared Volume Mount Detailed Explanation Request

**User**: Describe the shared Volume mount mechanism in detail

**Coding Agent**: Detailed explanation of shared Volume mount mechanism.

Basic principle: emptyDir shared directory, all containers in Pod can read/write.

Workflow:
1. When Controller creates Pod, creates ConfigMap containing Harness configuration
2. ConfigMap mounted as Volume to Pod's shared directory
3. Handler and Framework containers both mount the same Volume
4. Handler reads configuration, parses and starts Framework
5. When configuration changes, ConfigMap content updates → Volume content syncs
6. Handler polls or watches file changes, dynamically adapts

Pros and Cons:
- Pros: Simple, no permissions needed, no restart needed
- Cons: ConfigMap update has delay (~1 minute)

Does this solution match your expectation?

---

## 36. Confirm Shared Volume Solution

**User**: Adopting this solution, the only downside currently is ConfigMap size limit (1MB)

**Coding Agent**: Yes, 1MB limit is the main constraint.

Question analysis: Is 1MB enough?

Harness configuration mainly connection parameters, access methods, small volume. Estimated tens of KB to hundreds of KB, 1MB sufficient.

In your business scenario, what's the estimated configuration volume? Could 1MB become a bottleneck?

---

## 37. Confirm Configuration Size Sufficient

**User**: Various configurations are actually more about configuring server and how to access, so can consider adopting this method

**Coding Agent**: Okay, confirmed. Adopt shared Volume mount ConfigMap solution.

Next question: Configuration format

**Option A: YAML format**
**Option B: JSON format**
**Option C: Split into multiple files by Harness**

---

## 38. Configuration Format Decision

**User**: Option A: YAML format

**Coding Agent**: Okay, confirmed. Adopt single YAML file format.

Confirmed core design summary (updated).

Next question: Multiple AIAgents in one Runtime execution isolation

Handler needs to distinguish requests from different AIAgents.

**Option A: Handler unified entry, internal routing**
**Option B: Each AIAgent independent port**

---

## 39. Communication Entry Discussion

**User**: Communication method between Agents is decided by Agent framework, for example some use A2A, OpenClaw doesn't use A2A but connects to various social software, so this place shouldn't restrict communication mode and entry mode

**Coding Agent**: I understand. Platform layer only provides infrastructure capability (shared network namespace), communication mode and entry mode are decided by Handler and framework themselves.

Next question: Sandbox and AgentRuntime relationship

You initially mentioned "AIAgent and Sandbox are N:M relationship", need to clarify how Sandbox associates with AgentRuntime/AIAgent.

**Option A: Sandbox as independent Pod**
**Option B: Sandbox as AgentRuntime sidecar**
**Option C: Sandbox as Harness CRD definition, Handler decides how to interface**

---

## 40. Sandbox Form Discussion

**User**: Should support two ways: one is Sandbox as independent Pod resource (Sandbox defined by current k8s agent-sandbox project), also can be Handler and Agent Framework running inside the Sandbox defined by agent-sandbox, and inside Sandbox can also run code execution environment container

**Coding Agent**: I understand, Sandbox supports multiple forms.

Confirmed Sandbox forms:
- Independent Pod: Sandbox as independent resource, AgentRuntime calls through API
- Embedded: Handler+Framework runs inside Sandbox
- Hybrid: Sandbox simultaneously runs Handler+Framework + code execution container

Next question: How to express Sandbox form in CRD

**Option A: Configure Sandbox mode in Harness**
**Option B: AgentRuntime directly specifies Sandbox binding method**

---

## 41. Sandbox Pooling Discussion

**User**: Also need to note that Sandbox itself can be a resource pool, dynamically associate to use a specific Sandbox

**Coding Agent**: Okay, this introduces Sandbox pooling and dynamic scheduling concept.

New architecture understanding: SandboxPool (resource pool) → dynamic scheduling → AIAgent/AgentRuntime.

Next question: Sandbox instance lifecycle ownership

**Option A: Independent SandboxPool CRD management**
**Option B: Trigger Sandbox creation when AIAgent/AgentRuntime created**
**Option C: Both coexist**

---

## 42. Explore agent-sandbox Project

**User**: agent-sandbox project already has Sandbox and resource pool management, so how to utilize existing Sandbox CRD resources well, and also consider the running relationship between AI Agent and Sandbox mentioned earlier

**Coding Agent**: Need to first understand agent-sandbox project's existing Sandbox CRD design. I explored agent-sandbox subdirectory, this is agent-sandbox source code.

[Coding Agent explored agent-sandbox project's API definitions, found the following core CRDs:]

- Sandbox: Sandbox instance (Pod)
- SandboxTemplate: Sandbox template, defines Pod spec, network policy
- SandboxWarmPool: Warm pool, maintains pre-warmed Sandbox instances
- SandboxClaim: Sandbox claim, obtains Sandbox instance

SandboxClaim's WarmPool strategy:
- none: Don't use WarmPool, always create new Sandbox
- default: Select from all matching WarmPools (default)
- Specific pool name: Only obtain from specified WarmPool

Integration point with AI Agent architecture: Based on existing design, AIAgent/AgentRuntime can obtain Sandbox through SandboxClaim.

Next question: How to associate AgentRuntime with Sandbox

**Option A: AgentRuntime directly creates SandboxClaim**
**Option B: Reference existing Sandbox/SandboxClaim through Harness**
**Option C: Embedded Sandbox (AgentRuntime Pod itself is Sandbox)**

---

## 43. Sandbox Association Method Decision

**User**: Option B: Reference existing Sandbox/SandboxClaim through Harness; need to support independent Sandbox mode and embedded Sandbox mode

**Coding Agent**: Okay, confirmed.

Reference existing Sandbox/SandboxClaim through Harness, support two modes:
- Independent Sandbox
- Embedded Sandbox

Next question: Harness Sandbox type configuration structure

Are these two modes mutually exclusive (one Harness can only choose one), or can both be configured simultaneously?

---

## 44. Sandbox Mode Mutual Exclusion Decision

**User**: These two modes are mutually exclusive (one Harness can only choose one)

**Coding Agent**: Okay, confirmed. One Harness can only choose one Sandbox mode.

Next question: AgentRuntime and SandboxTemplate relationship in embedded mode

**Option A: AgentRuntime references SandboxTemplate, Pod spec led by SandboxTemplate**
**Option B: AgentRuntime defines Pod spec, SandboxTemplate only provides security policy**

---

## 45. SandboxTemplate Reference Decision

**User**: Option A: AgentRuntime references SandboxTemplate, Pod spec led by SandboxTemplate

**Coding Agent**: Okay, confirmed.

In embedded mode: SandboxTemplate defines Pod base spec, AgentRuntime's Handler and Framework containers overlay to Pod.

Next question: Container overlay merge rules

**Option A: Append mode**
**Option B: Replace mode**
**Option C: User specifies merge strategy**

---

## 46. Container Merge Strategy Decision

**User**: Option A: Append mode

**Coding Agent**: Okay, confirmed.

Append mode: SandboxTemplate containers + AgentRuntime containers (Handler, Framework).

Confirmed design summary (updated).

Next question: AgentRuntime CRD complete structure

Need other fields? Such as:
- replicas (replica count)
- resources (resource limits)
- nodeSelector/affinity (node scheduling)

---

## 47. AgentRuntime CRD Complete Example Confirmation

**User**: Can add these fields as examples, initial design is basically complete

**Coding Agent**: Provide AgentRuntime CRD complete example, AIAgent CRD complete example, Harness CRD complete example.

Initial design completion confirmation: Any other questions need discussion?

---

## 48. Document Recording Request

**User**: Write the entire discussion and design into documents, provide Chinese and English versions each, explain each design's consideration factors clearly in detail, and summarize what goals can be achieved through this design

**Coding Agent**: Creating complete design documents, Chinese and English versions each.

Documents created:
- docs/design/ai-agent-architecture-design-CN.md (Chinese)
- docs/design/ai-agent-architecture-design-EN.md (English)

---

## 49. Conversation Recording Request

**User**: Also record the entire conversation process, including User and Coding Agent's responses, as original prompt record. Store in Chinese and English files each

**Coding Agent**: Creating conversation record files.

---

## 50. File Renaming Request

**User**: Rename the design document filenames to be shorter: change ai-agent-architecture-design to ai-agent-design

**Coding Agent**: Files renamed.

| Original Filename | New Filename |
|-------------------|--------------|
| ai-agent-architecture-design-CN.md | ai-agent-design-CN.md |
| ai-agent-architecture-design-EN.md | ai-agent-design-EN.md |

---

## 51. Conversation Document Separation Request

**User**: Move conversation documents to docs/conversation subdirectory, separate from design subdirectory. And shorten filename ai-agent-conversation to conversation

**Coding Agent**: Completed.

Final file structure:
```
docs/
├── design/
│   ├── ai-agent-design-CN.md
│   └── ai-agent-design-EN.md
└── conversation/
│   ├── conversation-CN.md
│   └── conversation-EN.md
```

---

## 52. MCP Registry Mode Discussion

**User**: In Harness, should MCP example only configure MCP Server registry, and allowed/denied MCP Servers for discovery? Otherwise if only configuring specific MCP servers, Harness cannot enumerate all MCP servers, and Handler cannot do standardized processing

**Coding Agent**: Good point. Current design has the problem that MCP servers cannot be enumerated, and Handler cannot standardize processing of specific servers.

I'll update design documents to adopt MCP Registry mode configuration.

**MCP Harness Registry Configuration Structure**:

```yaml
spec:
  type: mcp
  mcp:
    registry:
      endpoint: https://mcp-registry.example.com
      authSecretRef: mcp-registry-token
    allowedServers:     # Allowed MCP Server whitelist
      - github
      - browser
      - filesystem
    deniedServers:      # Denied MCP Server blacklist
      - dangerous-tool
    discoveryPolicy: allowlist  # Discovery policy: allowlist | denylist | all
```

**Key Design Considerations**:
- MCP Server count is huge and cannot be enumerated
- Harness only configures Registry not specific Servers
- Handler standardizes Registry connection and Server discovery mechanism
- Specific Servers dynamically decided by Agent business, obtained through Registry
- Whitelist/blacklist policies control available Server scope

---

## 53. Refresh Conversation Document Request

**User**: Refresh the newly added related conversations into Chinese and English conversation documents

**Coding Agent**: Updating conversation documents, adding MCP Registry mode discussion content.

---

## 54. agentConfig Enhancement Design Discussion Start

**User**: Now consider another enhancement. Some agent frameworks need to inject information like prompt, communication protocol A2A, or agent registry for capability discovery when starting agent. How should we enhance the existing design? Let's determine step by step through interaction.

**Coding Agent**: Okay, starting new design discussion. First clarify the scope of requirements.

Information to inject:
- Prompt: Agent's prompt/instruction
- Communication protocol: e.g., A2A (Agent-to-Agent) protocol config
- Agent Registry: for capability discovery

First question: Injection timing

**Option A**: Inject when AIAgent CRD created
**Option B**: Runtime dynamic injection
**Option C**: Both coexist

---

## 55. Injection Timing Decision

**User**: Both coexist. Would file or configuration information delivery to handler be more appropriate? Should it be in CRD or a dedicated harness to handle this initialization and possibly update processing?

**Coding Agent**: Okay, confirmed both coexist mode.

Next question: Delivery method

Two dimensions can be combined:
1. File vs configuration information delivery to Handler
2. CRD embedded vs dedicated Harness processing

Expand detailed discussion of options C and D (file delivery).

---

## 56. File Delivery Method Discussion

**User**: Expand discussion of C and D options separately

**Coding Agent**: Detailed expansion of options C and D:

**Option C: File delivery + AIAgent CRD embedded**
- File content directly embedded in AIAgent CRD
- Controller automatically creates ConfigMap to store
- Cannot reuse, each Agent has independent configuration

**Option D: File delivery + Dedicated Harness CRD**
- AgentConfig as independent Harness type
- AIAgent references Harness, can override partial configuration
- Supports multiple Agents sharing configuration template

Comparison summary: Option C for Agent config independent, Option D for multiple Agents sharing configuration template.

---

## 57. More Abstract Design Approach

**User**: Still not abstract enough. Actually just define a file delivery method, specific file content format decided by Handler, so users specify related content in files per Handler format, then can pass what Handler needs to start agent to agent framework.

**Coding Agent**: Understood, this is a more abstract approach, aligns with Handler pattern design philosophy.

Core approach: Platform layer only defines file delivery mechanism, specific file content format decided by Handler, users provide content per Handler requirements.

Next question: File source

**Option A**: Directly embed file content in AIAgent CRD
**Option B**: Reference external ConfigMap/Secret
**Option C**: Both coexist

---

## 58. File Source Decision

**User**: Reference external ConfigMap/Secret

**Coding Agent**: Okay, confirmed using reference to external ConfigMap/Secret.

Next question: Mount path specification

**Option A**: Unified mount path
**Option B**: User specifies mount path
**Option C**: Unified path + subdirectory naming specification

---

## 59. Mount Path Decision

**User**: Option A: Unified mount path

**Coding Agent**: Okay, confirmed using unified mount path.

- Unified mount path: `/etc/agent-config/`
- Subdirectories created by reference name
- Handler knows to read all config files from this path

Next question: Runtime update mechanism

**Option A**: Handler actively monitors file changes
**Option B**: Platform notifies Handler
**Option C**: Restart Pod

---

## 60. Update Mechanism Decision

**User**: Option A: Handler actively monitors file changes

**Coding Agent**: Okay, confirmed Handler actively monitors file changes.

- Handler uses fsnotify or polling to monitor `/etc/agent-config/` directory
- When files change, Handler reloads configuration and updates Agent
- Handler decides update strategy itself

Next question: Multiple config files priority

**Option A**: Priority by reference order
**Option B**: User explicitly specifies priority
**Option C**: Don't handle, decided by Handler

---

## 61. File Conflict Handling Decision

**User**: Files should have different names, config items handled by Handler. Handler theoretically avoids same names when defining config file content

**Coding Agent**: Okay, confirmed.

- Handler responsible for defining config file naming specification, avoid same names
- Users prepare differently named files per Handler requirements
- Platform layer doesn't handle file conflicts, only responsible for mounting

Next question: ConfigMap/Secret declaration method

**Option A**: Declare in AIAgent CRD
**Option B**: Declare public config in AgentRuntime CRD, AIAgent can append
**Option C**: Only declare in AIAgent, Runtime not involved

---

## 62. Declaration Method Decision

**User**: Option B: Declare public config in AgentRuntime CRD, AIAgent can append. Because some configs are same for all agents of this type, while some are AI agent specific. Current examples are just examples.

**Coding Agent**: Okay, confirmed.

- AgentRuntime CRD declares public config (for all Agents of same type)
- AIAgent CRD appends Agent-specific config
- Similar to Harness inheritance mode

Next question: Override behavior when AIAgent appends config

**Option A**: AIAgent overrides Runtime config
**Option B**: Merge mount (both mounted)
**Option C**: User explicitly specifies override or append

---

## 63. Override Behavior Decision

**User**: Option B: Merge mount (both mounted). Handler determines processing logic, has maximum flexibility.

**Coding Agent**: Okay, confirmed.

- Runtime public config mounted to `/etc/agent-config/runtime/`
- AIAgent config mounted to `/etc/agent-config/agent/`
- Handler decides merge logic itself, has maximum flexibility

Next question: Does AgentRuntime level agentFiles also support dynamic update

**Option A**: Support dynamic update
**Option B**: Not supported, immutable after creation
**Option C**: Supported but requires user confirmation

---

## 64. Runtime Dynamic Update Decision

**User**: Option A: Support dynamic update. Handler decides update method.

**Coding Agent**: Okay, confirmed.

- AgentRuntime's agentFiles supports dynamic update
- Handler monitors file changes and decides update method

Next question: ConfigMap/Secret reference scope

**Option A**: Only reference ConfigMap/Secret in same Namespace
**Option B**: Support cross-Namespace reference
**Option C**: Both coexist, default same Namespace, optionally specify

---

## 65. Reference Scope Decision

**User**: Option A: Only reference ConfigMap/Secret in same Namespace. Considering multi-tenancy support, cross-Namespace reference only considered when actual use case arises.

**Coding Agent**: Okay, confirmed.

- Only reference ConfigMap/Secret in same Namespace
- Aligns with multi-tenant isolation principle
- Cross-Namespace reference considered when actual use case needs arise

agentFiles design summary confirmed.

---

## 66. CRD Structure Example Confirmation

**Coding Agent**: Provide agentFiles CRD structure example and Pod mount structure.

**User**: Structure basically matches expectation. Note the difference between harness and config. Harness is external platform engineering capabilities built around AI agent, external capabilities like observability, security, traffic governance can be fine-grained processed by agent ID; while config doesn't involve platform engineering capabilities, mainly configuration information needed by AI agent/handler/framework.

**Coding Agent**: Okay, this is important concept clarification.

Harness vs agentFiles distinction:
- Harness: Platform engineering capabilities, platform layer manages and provides
- agentFiles: Business configuration, Handler decides format and usage

Next question: Naming confirmation

**Option A**: Keep agentFiles naming
**Option B**: Use agentConfig naming
**Option C**: Use other naming

---

## 67. Naming Decision

**User**: Option B: Use agentConfig naming

**Coding Agent**: Okay, confirmed. Use agentConfig naming.

Provide final CRD structure example, including distinction between harness and agentConfig.

agentConfig design summary confirmed.

---

## 68. Document Update Request

**User**: Update Chinese and English design documents, especially clearly explain the different purposes and design considerations of harness and config. Also update Chinese and English conversation documents.

**Coding Agent**: Updating design documents and conversation documents, adding Harness vs agentConfig concept distinction and agentConfig design chapter.

**Coding Agent**: Creating conversation record files.

---

## 69. Chapter Structure Adjustment Request

**User**: Merge Sandbox Integration Design into AgentRuntime Design as a subsection of Agent Runtime. Merge agentConfig Design into AI Agent Design as a subsection of AIAgent Design. Chapters Harness vs agentConfig Concept Distinction and Harness Configuration Delivery Mechanism should both be subsections under Harness Design, pay attention to logical order.

**Coding Agent**: Okay, I'll adjust the chapter structure:
- Sandbox Integration Design as subsection 3.4 in AgentRuntime Design
- agentConfig Design as subsection 4.6 in AIAgent Design
- Harness vs agentConfig Concept Distinction as subsection 5.1 in Harness Design
- Harness Configuration Delivery Mechanism as subsection 5.8 in Harness Design

Also need to adjust subsequent chapter numbers and agentConfig fields in CRD examples.

---

## 70. Remove Analogy Relationship Table

**User**: Remove the Analogy Relationship section from both Chinese and English documents

**Coding Agent**: Analogy Relationship table removed from both Chinese and English documents.

---

## 71. Agent Framework Running Modes Design Request

**User**: AgentRuntime Design should add support for several running modes of agent framework: long running service, event-triggered on-demand execution, external presentation as server mode (listening on port) and client mode (similar to chat software client, like OpenClaw, WeChat's weixin-claw)

**Coding Agent**: Okay, I'll add Agent Framework Running Modes in AgentRuntime Design section:

#### Lifecycle Modes
- Long Running: Long-running service, continuously providing service capabilities
- Event-triggered: Event-triggered on-demand execution, terminates after task completion

#### Communication Modes
- Server Mode: Listening on port, providing services externally
- Client Mode: Actively connects to external services, similar to chat client

Also adjust subsequent chapter numbers:
- 3.3 Pod Container Configuration → 3.4 Pod Container Configuration
- 3.4 Sandbox Integration Design → 3.5 Sandbox Integration Design
- 3.5 CRD Structure Example → 3.6 CRD Structure Example

---

## 72. Update Client Mode Example Diagram

**User**: Client mode example should be modified to:
```
AgentRuntime Pod
│
└── Agent Framework
    └── AIAgent (OpenClaw / weixin-claw) ──► Connects to external platform service
                                              │
                                              ▼
                                        WhatsApp / Discord / WeChat...
```

**Coding Agent**: Modification completed. Client mode example diagram updated:
- AIAgent labeled with specific framework type (OpenClaw / weixin-claw)
- Connection target changed to WhatsApp / Discord / WeChat chat platforms

---

## 73. Conversation Record Update Request

**User**: Update conversation records to corresponding conversation files

**Coding Agent**: Updating conversation record files, appending current conversation content.

---

## 74. Resource Efficiency Considerations Section Request

**User**: Add a section about resource efficiency considerations in AgentRuntime. For many AIAgents, they are mostly idle, only busy when tasks arrive. Some AIAgents run short tasks, some run long tasks. Through lightweight agent handler, running agent framework in various ways, multiple AI Agents share one Pod's resources for resource sharing, improving resource utilization efficiency.

**Coding Agent**: Okay, I'll add Resource Efficiency Considerations section (3.4) in AgentRuntime Design, including:

#### Resource Usage Characteristics
- Long idle time: Agent mostly idle
- Task burstiness: Resource usage spikes when task arrives
- Task duration variance: Short and long tasks coexist
- Resource demand fluctuation: Varying demands across tasks

#### Resource Sharing Strategies
- Multi-Agent shared Pod resources: Idle Agents share infrastructure overhead
- Group by task type: Short task group, long task group, mixed group

#### Resource Efficiency Quantitative Analysis
Comparing traditional mode with shared mode, shared mode achieves 60%~80% utilization

#### Design Points
- Lightweight Agent Handler
- Framework process sharing
- Dynamic resource scheduling
- Pod-level resource quota

Also adjust subsequent chapter numbers:
- 3.4 Pod Container Configuration → 3.5 Pod Container Configuration
- 3.5 Sandbox Integration Design → 3.6 Sandbox Integration Design
- 3.6 CRD Structure Example → 3.7 CRD Structure Example

---

## 75. Modify Resource Efficiency Section

**User**: Delete #### 3.4.3 Resource Efficiency Quantitative Analysis, and consider two modes that AgentRuntime supports for multi-AI Agent to improve resource efficiency: Single Agent Framework Multiple AI Agents, and each Agent Framework one AI Agent but through multi-Agent Framework multi-process to implement multi-Agent.

**Coding Agent**: Okay, I'll modify:

1. Delete 3.4.3 Resource Efficiency Quantitative Analysis section
2. Modify 3.4.2 Resource Sharing Strategies to two AgentRuntime supported multi-AI Agent modes:

**Mode 1: Single Agent Framework Multiple AI Agents**
- One Agent Framework process hosts multiple AI Agents
- Process-level resource sharing: memory, network connections, runtime environment
- Framework internally implements Agent scheduling and resource allocation

**Mode 2: Multi Agent Framework Processes Multiple AI Agents**
- Each Agent Framework process corresponds to one AI Agent
- Multi-Agent through multi-process, shared Pod infrastructure and resource quota
- Process-level isolation, single Agent failure doesn't affect other Agents

Add comparison analysis table showing differences in process count, resource sharing granularity, isolation strength, resource efficiency between the two modes.

---

## 76. Modify Description

**User**: Change this sentence: Agent framework natively supports multi-Agent (e.g., CrewAI, ADK multi-Agent) -> Agent framework natively supports single-process multi-Agent

**Coding Agent**: Modification completed.

---

## 77. Delete Description

**User**: Delete this sentence: Tight Agent collaboration, need shared context

**Coding Agent**: Deleted. Both Chinese and English documents removed this sentence.

---

## 78. Add Model as Platform Service Capability

**User**: Add model as platform service capability in Harness

**Coding Agent**: Added model type in Harness design:

**Standard Type List Addition**:
- model: Model service, LLM model integration configuration

**Model Type Spec Structure**:
```yaml
spec:
  type: model
  model:
    provider: openai          # Model provider
    endpoint: https://api.openai.com/v1
    authSecretRef: openai-api-key
    models:                   # Available model list
      - name: gpt-4
        allowed: true
        rateLimit: 100
      - name: gpt-3.5-turbo
        allowed: true
      - name: o1-preview
        allowed: false
    defaultModel: gpt-4
```

**Considerations**:
- Model service is Agent's core dependency, needs unified configuration management
- Platform layer provides model integration capability, Handler doesn't handle different provider details
- Multi-model configuration and access control supported
- Authentication info managed through Secret

Also updated:
- AgentRuntime CRD example added model harness reference
- Harness CRD example added model type example
- Chapter numbering adjusted (5.3.1 Model Type Design, 5.3.2 MCP Type, 5.3.3 Other Types)

---

## 79. Delete Comment and Use Open Source Model Example

**User**: Delete this comment # Model provider: openai | anthropic | azure | local | custom, and use open source model as example, like deepseek, qwen

**Coding Agent**: Modification completed:

1. **Delete comment**: Removed provider field comment explanation

2. **Use open source model example**:
   - provider: deepseek
   - endpoint: https://api.deepseek.com/v1
   - models: deepseek-chat, deepseek-coder, qwen-turbo, qwen-max
   - defaultModel: deepseek-chat

3. **Updated all related locations**:
   - Spec structure example (5.3.1 section)
   - Harness CRD example (5.9 section)
   - AgentRuntime harness reference
   - metadata.name: model-openai-default → model-deepseek-default

---

## 80. Harness Chapter Title Modification

**User**: Change the chapter title "5. Harness Design" to "5. Harness Design(WIP and TBD)"

**Coding Agent**: Modification completed:
- Chinese: `## 5. Harness设计` → `## 5. Harness设计（WIP and TBD）`
- English: `## 5. Harness Design` → `## 5. Harness Design (WIP and TBD)`

---

## 81. Add Problems and Challenges Section Before Overview

**User**: Add ### 1.0 Problems and Challenges at the beginning of the document overview

#### 1.0.1 AI Agent Resource Utilization Efficiency Problem

AI Agents have new resource usage characteristics:
- Long idle time
- Task burstiness
- Task duration variance
- Resource demand fluctuation

AI Agent and execution environment have diverse considerations: merged or separated.

When Kubernetes cluster runs large-scale AI Agents, improving resource utilization efficiency is a common problem.

#### 1.0.2 AI Agent Technology Rapid Iteration, Platform Engineering Cannot Keep Up

From Langchain to Manus to OpenClaw, Hermes, technology frameworks keep evolving. CNCF/Kubernetes platform engineering is still built on traditional Pod, microservices, service mesh. Need to solve AI Agent granularity perception problem.

**Coding Agent**: Added "1.0 Problems and Challenges" section at the beginning of overview, including:
- 1.0.1 AI Agent Resource Utilization Efficiency Problem
- 1.0.2 AI Agent Technology Rapid Iteration Problem

Chapter structure becomes:
- ## 1. Overview
  - ### 1.0 Problems and Challenges (new)
    - #### 1.0.1 Resource Utilization Efficiency Problem
    - #### 1.0.2 Technology Iteration Problem
  - ### 1.1 Purpose
  - ### 1.2 Core Objectives

---

## 82. Modify Purpose Section Content

**User**: Modify "1.1 Purpose" content:
- Agent framework list: LangChain, Sematic Kernel, OpenClaw, Hermes, etc.
- Add "future unknown Agent frameworks"
- Add "Model" at beginning of scaffolding capabilities list
- Change "AI Agent ID" to "AI Agent ID/Name"

**Coding Agent**: Modification completed:

Original:
- Frameworks: LangChain, ADK, OpenClaw, CrewAI, Hermes, etc.
- Capabilities: CLI Tools, MCP, Skills...
- Connection: AI Agent ID

Modified:
- Frameworks: LangChain, Sematic Kernel, OpenClaw, Hermes, etc. + future unknown Agent frameworks
- Capabilities: Model, MCP, Skills...
- Connection: AI Agent ID/Name

---

## 83. Delete Core Objectives Section

**User**: Remove ### 1.2 Core Objectives section

**Coding Agent**: Deleted "### 1.2 Core Objectives" section. Chapter structure becomes:
- ## 1. Overview
  - ### 1.0 Problems and Challenges
  - ### 1.1 Purpose
- ## 2. Core Object Definitions

---

## 84. Add Disclaimer Note

**User**: Add a note at the end of the document: The opinions expressed in this article do not reflect the view of the author's affiliation

**Coding Agent**: Added note at the end of document:
- Chinese: **注：本文所表达的观点不代表作者所属机构的立场。**
- English: **Note: The opinions expressed in this article do not reflect the view of the author's affiliation.**

---

## 85. Modify 1.0 Section Title and Add Typical Scenario

**User**:
1. Change ### 1.0 title to ### 1.0 Scenarios and Challenges
2. Add #### 1.0.1 Typical Scenario section about Alice's AI Agent startup connecting through WeChat

**Coding Agent**: Modification completed:

1. **Title change**:
   - Chinese: ### 1.0 问题和挑战 → ### 1.0 场景和挑战
   - English: ### 1.0 Problems and Challenges → ### 1.0 Scenarios and Challenges

2. **Added Typical Scenario section (1.0.1)**:
   - WeChat opened AI Agent interface, users can scan QR code to add AI Agent as friend
   - Alice as one-person startup developed life assistant AI Agent
   - Connecting through WeChat lowers promotion and operations costs
   - Facing resource cost issues: single process multi-Agent, Agent/Sandbox separation for reuse
   - User activity varies greatly, needs dynamic consolidate, scale up/down
   - Needs AI Agent granularity platform engineering

3. **Section numbering adjusted**:
   - Original 1.0.1 → 1.0.2 (Resource Utilization Efficiency Problem)
   - Original 1.0.2 → 1.0.3 (Technology Iteration Problem)

---

## 86. Modify Expressions in Typical Scenario of English Document

**User**: Modify three expressions in English document:
1. "WeChat recently opened an AI Agent interface similar to OpenClaw" → "WeChat recently opened an weixin-claw plugin for AI Agent like Openclaw or any other AI Agents to be accessible in WeChat"
2. "One user exclusively occupying" → "One AI Agent exclusively occupying"
3. "If users grow to millions or tens of millions" → "If AI Agents grow to millions or tens of millions"

**Coding Agent**: Modification completed:

1. **weixin-claw plugin description**: More accurately describes WeChat's weixin-claw plugin that enables AI Agents like OpenClaw or others to be accessible in WeChat
2. **Resource occupancy subject correction**: Changed from "user exclusively occupying" to "AI Agent exclusively occupying", more accurately describing resource allocation granularity
3. **Scale growth subject correction**: Changed from "users grow" to "AI Agents grow", more accurately describing business scale

---

## 87. Add Footnote

**User**: Add a footnote at the end of the document. When other documents reference the scenario, credit should be given to the article author using common/typical footnote format.

**Coding Agent**: Footnote added at the end of document:
- Chinese: **脚注**：如在其他文档或文章中引用本文"1.0.1 典型场景"章节的案例描述，请注明出处及本文作者。
- English: **Footnote**: When referencing the "1.0.1 Typical Scenario" section in other documents or articles, please cite the source and credit the author of this article.

---

## 88. Sync English Document Manual Updates to Chinese Document

**User**: The ai-agent-abstraction-EN.md document was manually refreshed, please reload and sync the changes from the English document to the Chinese document.

**Coding Agent**: Synced the English document changes to the Chinese document:

1. **Section numbering adjustment**:
   - 1.0 Scenarios and Challenges → 1.1 Scenarios and Challenges
   - 1.0.1 Typical Scenario → 1.1.1 Typical Scenario
   - 1.0.2 → 1.1.2
   - 1.0.3 → 1.1.3
   - 1.1 Purpose → 1.2 Design Purpose

2. **New content added**: Added description about resource utilization enhancement features at the end of section 1.2 Design Purpose:
   "To enhance resource utilization，AI agent abstraction should be able to support feature implementation like AI Agent bin pack conslidation, AI Agent migration, pod/node scale up/scale down, pod resize, sandbox reuse/hibernate/resize etc."

3. **Reference update**: Updated the reference in section 1.1.3 from "1.0.1" to "1.1.1"

4. **Footnote update**: Updated the section reference in footnote from "1.0.1" to "1.1.1"

---

## 89. Sync English Document AgentRuntime Section to Chinese

**User**: The English content updates for section 3. AgentRuntime Design should also be synced to the Chinese version.

**Coding Agent**: Synced the AgentRuntime section changes to the Chinese document:

1. **Paragraph update**: Added "AgentRuntime and AIAgent CRD lifecycles uniformly managed by AgentRuntime Controller, which is provided by platform."

2. **Structure adjustment**:
   - Removed the Q&A format "问题：如何避免为每种Agent框架开发独立Controller？"
   - Removed the decision format "决策：采用Agent Handler模式。"
   - Removed the separate "平台层Controller" definition

3. **Added Agent Framework definition**: Added the Agent Framework definition item
   "Agent Framework: Agent framework like LangChain, Sematic Kernel, OpenClaw, Hermes which run AI agent."

4. **Agent Handler description update**: Changed from "负责具体框架的启动、配置转换、Agent管理" to "responsible for specific framework startup, configuration conversion, and AI Agent lifecycle management."

---

## 90. Sync Comprehensive English Document Updates to Chinese

**User**: The ai-agent-abstraction-EN.md document was manually refreshed, please reload and sync all changes from the English document to the Chinese document.

**Coding Agent**: Fully synced all changes from the English document to the Chinese document:

**1.1.1 Typical Scenario section modifications**:
- Updated WeChat plugin description to generic AI Agent plugin
- Changed "web service security governance and operations" → "web service governance, security and operations"
- Changed "AI Agent and Sandbox separation" → "AI Agent and execution environment Sandbox separation"
- Added daily peak/off-peak fluctuation management description
- Updated scaling terminology (scaling out/in)

**1.1.2 section**:
- Added "or hybrid mode" to execution environment considerations

**1.2 Design Purpose section**:
- Added "ANY" emphasis words
- Added ADK to framework list
- Rewrote resource efficiency improvement techniques description

**3. AgentRuntime section**:
- Updated Controller description (framework agnostic)
- Added TBD: Agent Handler should not participate in operational traffic
- Updated process mapping mode diagrams (process instead of container)
- Updated multi-Agent instances description (same type or different type)
- Added consideration about Agent Framework process only supporting one externally visible Agent

**3.4 Resource Efficiency section restructuring**:
- Removed original 3.4.1 Resource Usage Characteristics standalone section
- Merged into 3.4.1 Resource Sharing Strategies
- 3.4.2 changed to Design Points
- Comparison table removed isolation strength column

**3.5/3.6.3 sections**:
- Added TBD: Agent Handler management mechanism design
- Added TBD: Seamless AI Agent switching while reusing same Sandbox

**New 3.7 section**:
- Agent identification management: CRD ID/Name mapping with framework internal UUID

**3.8 CRD example updates**:
- Removed resources fields
- Updated model name to model-deepseek-default

**4. AIAgent section modifications**:
- 4.1 opening changed to "critical for resource efficiency"
- 4.4 migration triggers added consolidation, modified example format
- 4.6.2 consideration changed to "controlled skill set"
- 4.7 CRD example changed to agent-skill-set

**5. Harness section modifications**:
- Added paragraph describing Handler as mediator
- 5.1.1 examples updated to GAIE Gateway etc.
- 5.1.2 Harness description updated

**7. Summary section**:
- Removed Pod/Node analogy
- Added resource efficiency purpose