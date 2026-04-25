# AI Agent抽象架构设计对话记录（中文版）

## 1. 初始评估请求

**用户**：先评估如下AI agent对象的抽象的可行性：
    目的：当前kubernets下缺少ai agent这个核心对象的抽象，希望这个核心对象能够类似pod这个核心资源一样，对任何agent框架，任何形式的agent，不管是langchain还是adk,还是openclaw，还是hermes agent等等都能够进行抽象，同时外置agent的各种脚手架处理能力，比如cli tools, mcp，skills, knowledge, rag, memory, state，guardrail, security, policy, gateway, sandbox等等，这些外置处理可以通过ai agent的id串接起来。注意对于一个AI agent来说，它可以跟sandbox执行环境的在同一个地方，也可以sandbox执行环境分离的，比如几个ai agent对应着同一个sandbox执行环境，但是实际执行的时候sandbox只执行一个ai agent的逻辑，也就是AI Agent和sandbox是N：M的对应关系。一个agent的框架进程，可以只对应一个ai agent资源id，也可以对应多个ai agent资源id。整体上要求AI agent要足够灵活的抽象，达到类似pod，进程这样具有高度面对未知业务的抽象能力。
    方案思路：对pod进行升级改造或者二次封装，一个pod里面有一个agent handler进程，这个agent handler有它自己的镜像，负责监控由该镜像负责的ai agent资源id，然后在pod里面，根据ai agent的框架选择，启动对应的进程(容器)，比如adk或者openclaw进程(容器)，在adk/openclaw通过配置或者其他方式，创建出对应的ai agent，并且和ai agent资源id关联，也就是最终ai agent id关联到agent框架进程，可能多个ai agent id关联到同一个进程id，也可能是一个ai agent id关联一个agent id，这个由agent handler的代码逻辑决定。

**Coding Agent**：我来从多个维度评估这个AI Agent抽象方案的可行性。

总体评估：方案思路**基本可行**，但有几个关键问题需要重新审视。

### 方案优点
- 统一抽象层：用CRD定义AI Agent资源，能对齐K8s生态
- 框架无关：Agent Handler作为sidecar/initializer模式，确实能解耦框架差异
- 外置能力模块化：将MCP/Skills/RAG等作为独立组件，通过Agent ID串联，符合微服务解耦思路
- N:M映射灵活：Agent与Sandbox解耦，支持资源池化

### 关键问题
1. Pod二次封装的定位不清晰
2. Agent Handler的角色边界模糊
3. N:M映射的运行时冲突
4. 外置能力的绑定机制缺失
5. 进程与Agent ID的映射逻辑缺失

### 核心建议
1. 用CRD定义AI Agent为一等资源，而非Pod二次封装
2. Agent Handler上移到Operator层
3. 设计CapabilityBinding机制处理外置能力
4. 先实现1:1隔离模型，再扩展N:M共享模型

需要我进一步细化某个部分的设计吗？

---

## 2. 逐个讨论请求

**用户**：每一个问题或者需要决策的点单独一个一个地讨论

**Coding Agent**：好的，我们逐个讨论。

### 问题1：Pod二次封装的定位

你提到"对Pod升级改造或二次封装"，这里有两种完全不同的技术路径：

**路径A：CRD + Operator模式**
- AI Agent是独立的一等资源，类似Deployment、StatefulSet
- Pod只是AI Agent的运行时载体
- Operator负责创建、监控、重建Pod

**路径B：Pod内嵌Handler模式**
- Agent Handler作为Pod内的"超级进程"
- 在Pod内部管理框架容器的生命周期

我的判断：路径A（CRD + Operator）更符合K8s设计哲学。

你的想法是什么？是想走路径A还是路径B？

---

## 3. Handler职责澄清

**用户**：这里面有个问题，如果采用CRD controller的方式，那么会导致开发职责的混淆。agent框架是由实际业务人员决策使用，因此管理和启动agent，也即是需要知道具体框架应该处理哪些agent的输入信息，比如prompt如何提供，工具如何提供，等等都只能由选择agent框架以及这个agent框架对应的handler才能具象化处理；采用crd controller，意味着每一种agent框架都需要独立的controller，但是对于平台层来说，更希望一个能够抽象任何agent框架类型比如openclaw和crewai, adk就是完全不同类型的agent框架，如果采用controller方案，就会每个agent框架，一个controller；而采用agent handler的方案，就可以由对应agent框架提供一个handler，但是ai agent crd资源的controller却可以统一抽象为一个类型的

**Coding Agent**：理解了，这是一个很好的区分。

你的核心观点：

| 层级 | 职责 | 谁负责 |
|------|------|--------|
| Controller | 平台层，统一抽象，管理AIAgent CRD生命周期 | 平台团队 |
| Agent Handler | 框架层，具象化处理prompt/tools/框架启动 | 框架提供者/业务开发者 |

这样Controller不需要知道ADK和CrewAI的区别，只需要知道"调用哪个Handler"。

确认理解：所以架构变成Controller创建Pod并注入Handler镜像，Handler由框架提供，知道如何启动框架进程和配置prompt/tools。

你的理解正确吗？

---

## 4. Handler与框架进程关系

**用户**：理解是对的，更倾向于选项A（Handler作为sidecar，框架进程是独立容器），但是对于容器隔离，能否减少厚重的程度，尽可能轻量化容器隔离带来的开销

**Coding Agent**：好，这个需求很实际。

### 容器隔离的开销来源
- 镜像体积：每个容器需要独立镜像
- 启动时间：容器启动需要几秒
- 内存占用：每个容器独立的进程空间
- 网络栈：容器间通信需要走localhost

### 轻量化方案对比

**方案1：共享进程命名空间（推荐）**
- 两个容器共享PID命名空间，可以互相看到对方进程
- 仍然保持文件系统、网络命名空间隔离
- Handler可以通过/proc监控框架进程，甚至发信号
- 开销：几乎无额外开销，只是K8s的一个配置

**方案2：单容器多进程**
- 只有一个容器，最轻量
- 问题：Handler和框架强耦合，镜像需要定制

你希望隔离粒度到什么程度？

---

## 5. 命名空间共享决策

**用户**：共享进程命名空间，同时对于网络空间，没有必要进行隔离，但是文件系统需要隔离，因为把agent handler和运行实际ai agent的框架进程，是看作紧耦合的进程间关系，但是只是为了独立发布镜像需要，可以独立发布和升级，但是没有特别的安全性隔离需求。考虑每个ai agent和框架进程可能是一对一关系或者多对一关系，因此每个ai agent需要有独立的文件空间是有必要的

**Coding Agent**：理解了，总结一下约束：

| 维度 | 选择 |
|------|------|
| 进程命名空间 | 共享 |
| 网络命名空间 | 共享 |
| 文件系统 | 隔离 |
| 镜像 | 独立发布 |

需要澄清的点：你提到"每个AI Agent需要有独立的文件空间"，这里假设场景是一个框架进程对应3个AI Agent ID（多对一）。这里的"独立文件空间"是指哪种？

**选项A：按AI Agent ID隔离目录**
**选项B：按AI Agent ID隔离Volume/PVC**

---

## 6. PVC粒度决策

**用户**：选项B是否更加灵活方便一些，在数据的备份恢复以及管理上

**Coding Agent**：是的，选项B的优势：

| 维度 | 优势 |
|------|------|
| 备份恢复 | 按Agent粒度独立备份，互不影响 |
| 扩容迁移 | Agent-1数据太大，可以独立迁移到更大的存储 |
| 生命周期 | Agent删除后，PVC可以选择保留或级联删除 |
| 权限控制 | 不同Agent的PVC可以设置不同的StorageClass/加密策略 |

确认方案：每个AI Agent CRD创建时，Controller自动创建对应的PVC。

下一个问题：当AI Agent删除时，PVC如何处理？

**选项A：级联删除**
**选项B：保留PVC**
**选项C：由用户在CRD中指定策略**

---

## 7. PVC生命周期策略

**用户**：用户选择

**Coding Agent**：好，确定。

已确认的方案：

| 维度 | 决策 |
|------|------|
| 进程命名空间 | 共享 |
| 网络命名空间 | 共享 |
| 文件系统 | 按Agent ID独立PVC |
| PVC生命周期 | 用户在CRD中指定策略 |

下一个问题：Handler与框架进程的通信

既然共享网络命名空间，Handler和框架进程可以通过localhost通信。需要确认通信方向。

---

## 8. 通信方式决策

**用户**：这种设计本身就是希望让agent handler自己决定使用什么方式在一个pod里面去管理agent和agent框架，所以这个地方是否留给agent handler会更好。但是提供任何可能性给agent handler和agent框架

**Coding Agent**：明白了。平台层只提供基础设施能力，具体通信方式由Handler和框架决定。

已确认：平台层不强制通信协议。

下一个问题：Handler镜像的来源

既然Handler负责"知道如何启动和管理框架"，那么Handler镜像由谁提供？

**选项A：框架社区提供**
**选项B：平台层提供通用Handler模板**
**选项C：两者并存**

---

## 9. Handler镜像来源

**用户**：A 框架社区提供，如果社区不提供，自然有使用这种框架的业务人员自己开发

**Coding Agent**：好，确定。

Handler镜像由框架生态负责提供，平台层不做假设。

下一个问题：CRD中如何指定Handler

用户创建AI Agent时，如何告诉系统使用哪个Handler？

**选项A：直接指定Handler镜像**
**选项B：指定框架类型，Handler自动推断**
**选项C：混合模式**

---

## 10. Handler指定方式

**用户**：选项A

**Coding Agent**：好，确定。

用户直接指定Handler镜像和Framework镜像，平台层不做推断。

下一个问题：Agent ID的生成

创建AIAgent CRD时，Agent ID如何确定？

**选项A：CRD名称即Agent ID**
**选项B：自动生成UUID**
**选项C：用户指定或自动生成**

---

## 11. Agent ID设计

**用户**：name和uuid都需要，name更适合人类阅读，但是唯一性应该由uuid决定，比如说同一个namesapce下面，可能用的ageng框架不同，但是名称相同，通过uuid还是可以唯一区分开。当然也可以决定name就是唯一的，由创建CR的人或工具确定。你建议哪一个

**Coding Agent**：建议name唯一。

理由：
- K8s惯例：用户熟悉
- kubectl体验：直接用name
- 复杂度：简单，无需额外字段

补充：K8s每个资源自动生成metadata.uid，如果需要绝对唯一标识，可以用metadata.uid作为Agent ID。

你觉得这样可以吗？

---

## 12. Agent ID确认

**用户**：可以，按照k8s惯例处理

**Coding Agent**：好，确定。

已确认：
- metadata.name：用户指定，namespace内唯一，人类可读
- metadata.uid：K8s自动生成，绝对唯一标识

Handler自行决定用哪个作为Agent ID。

下一个问题：多对一映射

你提到"多个AI Agent IDs可以对应一个框架进程"。谁决定多个Agent合并到一个进程？

**选项A：用户在CRD中显式指定**
**选项B：Handler自动决定**
**选项C：平台层不支持，由Handler内部实现**

---

## 13. 多对一映射决策（触发架构重构）

**用户**：最关键的问题，其实是如何关联多个ai agent到一个agent handler实例，也就是一个pod上，这个有什么建议。具体handler是一个框架进程一个agent id还是一个框架进程对应多个agent id，由agent handler决定

**Coding Agent**：这是核心架构问题。梳理几种方案：

**方案A：AgentGroup CRD（显式分组）**
**方案B：Selector模式（Label绑定）**
**方案C：AIAgent中指定目标Pod**
**方案D：Handler镜像作为分组Key**

你的倾向？

---

## 14. 架构重构提议

**用户**：是否应该是这样的关系：agent-handler和agent-framework作为一个合并的对象，agent-handler和pod是一一对应的，然后ai agent是另外一个对象，它可以被映射或者调度到agent-handler和agent-framework合并的那个对象上。通过ai agent和合并对象的解耦，可以使得ai agent动态调度或者迁移到不同的合并对象（底下是pod）上

**Coding Agent**：理解了，这个思路更清晰。

新的架构理解：

类比：
- AgentRuntime类似Node（运行时载体）
- AIAgent类似Pod（可被调度的工作负载）

下一个问题：谁负责调度？

**选项A：平台Controller调度**
**选项B：用户指定Runtime**
**选项C：混合模式**

---

## 15. 调度模式决策

**用户**：应该是混合模式，也就是在AI Agent里面指定runtime类型，则由controller自动调度，如果指定runtime实例，则固定绑定在runtime实例上

**Coding Agent**：好，确定。这与K8s的Node调度模式一致。

已确认调度模式：
- runtimeRef.type：自动调度
- runtimeRef.name：固定绑定

下一个问题：运行时迁移

你提到AIAgent可以动态迁移到不同Runtime。这涉及几个细节。

**选项A：用户主动触发**
**选项B：Controller自动迁移**
**选项C：两者都支持**

---

## 16. 迁移支持决策

**用户**：两者都支持

**Coding Agent**：好，确定。

下一个问题：迁移时数据/状态处理

迁移过程中，AIAgent的PVC、Memory、运行状态如何处理？

**选项A：PVC跟随迁移**
**选项B：数据复制迁移**
**选项C：状态重建**

---

## 17. PVC迁移决策

**用户**：选项A：PVC跟随迁移

**Coding Agent**：好，确定。

迁移时PVC跟随AIAgent，从旧Pod解绑并挂载到新Pod。

技术约束提醒：PVC跨节点迁移需要存储后端支持（网络存储如NFS、Ceph、Longhorn）。

这是否符合你的预期？

---

## 18. 确认存储约束

**用户**：符合预期

**Coding Agent**：好，继续下一个问题。

下一个问题：外置能力绑定

你最初提到外置能力（MCP、Skills、RAG、Memory、Guardrail等）通过Agent ID串接。

需要确认：外置能力绑定到AIAgent还是AgentRuntime？

---

## 19. 外置能力绑定讨论

**Coding Agent**：首先需要梳理所有的agent涉及的外置能力有哪些，哪些是agent特定需要跟agent id关联的，也就是每个ai agent会对应着不同的内容；有哪些是对于同一个agent runtime来说，所有的ai agent看到的都是一样的能力。

能力清单：
- MCP：工具/能力接入
- Skills：技能模块
- CLI Tools：命令行工具
- Knowledge/RAG：知识库、检索增强生成
- Memory：对话历史、长期记忆、上下文
- State：运行状态、任务进度
- Guardrail：输出约束、内容过滤、安全护栏
- Security：安全策略、认证授权
- Policy：策略控制、合规约束
- Gateway：API网关、流量入口
- Sandbox：执行隔离环境

请确认/补充：以上清单是否完整？哪些是Agent级别？哪些是Runtime级别？

---

## 20. 能力绑定模式

**用户**：应该这样，这些能力对于使用同一个agent runtime的所有ai agent来说，公共的配置能力是一样的，但是每个ai agent可以有自己的配置，比如允许使用什么能力，禁止使用什么能力，如果ai agent不做定制化，则是一样的，如果做定制化，则把ai agent的定制化 apply到公共能力之上，形成其定制化的能力

**Coding Agent**：理解了，这是一个继承+覆盖模式。

下一个问题：能力配置的优先级

如果AIAgent定制化的配置与Runtime公共配置冲突，如何处理？

**选项A：AIAgent优先**
**选项B：Runtime优先**
**选项C：策略控制**

---

## 21. 配置优先级决策

**用户**：配置如果有冲突，只要这个能力是可获得可实施的，以AI Agent为准

**Coding Agent**：好，确定。

已确认配置优先级：AI Agent定制化配置 > Runtime公共配置（前提：能力可获得可实施）。

下一个问题：能力校验

当AIAgent定制化配置请求某个能力时，如何校验其可获得性？

**选项A：Controller创建时校验**
**选项B：Handler运行时校验**
**选项C：两者结合**

---

## 22. 能力校验决策

**用户**：选项A

**Coding Agent**：好，确定。

Controller在创建AIAgent时校验能力是否可获得，不可用则拒绝创建。

下一个问题：能力资源本身的管理

MCP、Memory、RAG、Sandbox等能力本身如何定义？

**选项A：每种能力独立CRD**
**选项B：统一Capability CRD**
**选项C：能力作为Runtime/AIAgent的内嵌配置**

---

## 23. 能力CRD设计讨论

**用户**：agent handler的意义就是通过相同的模式来支持不同的agent框架，因此是否应该在agent runtime的CRD中标准化各种能力定义

**Coding Agent**：好观点。这样Handler的核心价值才能体现。

Handler读取标准化配置，转换为各自框架需要的格式。

下一个问题：能力定义的共享

如果能力定义嵌入Runtime CRD中，多个Runtime需要使用同一个MCP实例时，如何处理？

**选项A：每个Runtime独立定义配置**
**选项B：引用外部能力实例**

---

## 24. 能力引用方式讨论

**用户**：考虑到ai agent还有可能定制化能力，因此是否应该是一个独立的，但是比标准化的能力集CRD会更为合适，这样agent handler和agent本身都可以引用

**Coding Agent**：理解了，这样设计更合理。

已确认：能力作为独立CRD。

下一个问题：Capability CRD的scope

**选项A：Namespace级**
**选项B：集群级**

---

## 25. Capability Scope讨论

**用户**：考虑多租户，还是namespace级别更合适，但是也不排除集群级别这种使用模式

**Coding Agent**：好，这与K8s的Role/ClusterRole模式一致。

已确认：两级Capability（Capability和ClusterCapability）。

下一个问题：引用规则

**选项A：自动查找**
**选项B：显式指定级别**

---

## 26. 术语调整和Scope决策

**用户**：不要使用capability这个词来代表agent的各种能力，使用harness来代表ai agent/agent handler的各种脚手架。查找方式采用k8s的惯例

**Coding Agent**：好，改用Harness。

CRD命名调整：
- Harness（Namespace级）
- ClusterHarness（集群级）

你说的"K8s惯例"具体是指哪种模式？

---

## 27. 去掉集群级Harness

**用户**：要不去掉集群共享模式，考虑多租户

**Coding Agent**：好，确定。简化设计，只保留Namespace级Harness。

多租户场景下，每个Namespace独立管理自己的Harness资源。

下一个问题：Harness类型标准化

需要确认有哪些标准类型。

---

## 28. Harness类型列表确认

**用户**：后面有可能有需要扩展的类型，比如增加structured output，先按照目前这些已知的来

**Coding Agent**：好，确定。

当前支持的类型（后续可扩展）：mcp、skills、cli-tools、knowledge、memory、state、guardrail、security、policy、gateway、sandbox。

下一个问题：Harness CRD的spec结构

**选项A：统一spec，用type区分**
**选项B：每种类型独立spec字段**
**选项C：混合模式**

---

## 29. Harness Spec结构决策

**用户**：选项B：每种类型独立spec字段

**Coding Agent**：好，确定。

下一个问题：多类型支持

一个Harness CRD是否可以同时配置多个类型？

**选项A：单一类型**
**选项B：多类型组合**

---

## 30. 单一类型决策

**用户**：选项A：单一类型

**Coding Agent**：好，确定。

一个Harness CRD只配置单一类型，通过type字段标识。

下一个问题：AgentRuntime与Harness的绑定关系

**选项A：引用Harness名称列表**
**选项B：按类型分组引用**

---

## 31. Harness引用方式决策

**用户**：选项B：按类型分组引用

**Coding Agent**：好，确定。

下一个问题：AIAgent的定制化覆盖

具体如何表达？

**选项A：引用+配置覆盖**
**选项B：allow/deny模式**

---

## 32. AIAgent定制化表达决策

**用户**：选项A：引用+配置覆盖

**Coding Agent**：好，确定。

下一个问题：AIAgent追加额外Harness

AIAgent是否可以引用不在AgentRuntime中的额外Harness？

**选项A：允许追加**
**选项B：不允许追加**

---

## 33. 不允许追加决策

**用户**：选项B：不允许追加

**Coding Agent**：好，确定。

AIAgent只能覆盖或禁止AgentRuntime中已有的Harness，不能额外追加新Harness。

下一个问题：Handler如何获取Harness信息

Handler启动时需要知道当前Runtime关联了哪些Harness。

**选项A：Handler通过API查询CRD**
**选项B：Controller注入配置**
**选项C：混合模式**

---

## 34. 配置传递方式讨论

**用户**：当前sidecar对于这种配置是如何处理的呢。是否可以参考sidecar的处理方式

**Coding Agent**：梳理一下K8s常见的sidecar配置处理方式：

Sidecar配置处理模式：
- Istio Envoy：xDS API动态推送
- Dapr：HTTP API获取配置
- Fluent Bit：ConfigMap挂载
- Vault Agent：API动态获取

三种典型模式：
- 模式A：共享Volume挂载
- 模式B：动态API推送
- 模式C：Handler主动查询

你倾向哪种？

---

## 35. 共享Volume挂载详细说明请求

**用户**：共享Volume挂载的机制详细描述一下

**Coding Agent**：详细说明共享Volume挂载机制。

基本原理：emptyDir共享目录，Pod内所有容器可读写。

工作流程：
1. Controller创建Pod时，创建ConfigMap包含Harness配置
2. ConfigMap作为Volume挂载到Pod的共享目录
3. Handler和Framework容器都挂载同一个Volume
4. Handler读取配置，解析后启动Framework
5. 配置变更时，ConfigMap内容更新 → Volume内容同步更新
6. Handler轮询或watch文件变更，动态适配

优缺点：
- 优点：简单、无需权限、无需重启
- 缺点：ConfigMap更新有延迟（~1分钟）

这种方案符合你的预期吗？

---

## 36. 确认共享Volume方案

**用户**：采用这种方案目前看唯一缺点就是ConfigMap大小限制（1MB）

**Coding Agent**：是的，1MB限制是主要约束。

问题分析：1MB是否够用？

Harness配置主要是连接参数、访问方式，体积较小。预估几十KB到几百KB，1MB足够。

你的业务场景中，预估配置体积会是怎样的？1MB是否可能成为瓶颈？

---

## 37. 确认配置大小足够

**用户**：各种配置实际上更多是配置server和如何访问，因此可以考虑采用这种方式

**Coding Agent**：好，确定。采用共享Volume挂载ConfigMap方案。

下一个问题：配置格式

**选项A：YAML格式**
**选项B：JSON格式**
**选项C：按Harness拆分多个文件**

---

## 38. 配置格式决策

**用户**：选项A：YAML格式

**Coding Agent**：好，确定。采用单一YAML文件格式。

已确认的核心设计汇总（更新）。

下一个问题：多个AIAgent在一个Runtime中的执行隔离

Handler需要区分不同AIAgent的请求。

**选项A：Handler统一入口，内部路由**
**选项B：每个AIAgent独立端口**

---

## 39. 通信入口讨论

**用户**：agent之间的通信方式由agent框架决定，比如有的采用a2a，openclaw就不用a2a，而是链接到各种社交软件，所以这个地方不应该限制通信模式和入口模式

**Coding Agent**：理解了。平台层只提供基础设施能力（共享网络命名空间），通信模式和入口模式由Handler和框架自行决定。

下一个问题：Sandbox与AgentRuntime的关系

你最初提到"AIAgent和Sandbox是N:M关系"，需要澄清Sandbox如何与AgentRuntime/AIAgent关联。

**选项A：Sandbox作为独立Pod**
**选项B：Sandbox作为AgentRuntime的Sidecar**
**选项C：Sandbox作为Harness CRD定义，由Handler决定如何对接**

---

## 40. Sandbox形态讨论

**用户**：应支持两种方式，一种是sandbox是独立pod资源（当前k8s agent-sandbox项目定义的sandbox），也可以是handler和agent framework就运行在agent-sandbox定义的sanbox里面，同时在sandbox内也可以运行代码执行环境容器

**Coding Agent**：理解了，Sandbox支持多种形态。

已确认Sandbox形态：
- 独立Pod：Sandbox作为独立资源，AgentRuntime通过API调用
- 嵌入式：Handler+Framework运行在Sandbox内部
- 混合式：Sandbox内同时运行Handler+Framework + 代码执行容器

下一个问题：如何在CRD中表达Sandbox形态

**选项A：Harness中配置Sandbox模式**
**选项B：AgentRuntime直接指定Sandbox绑定方式**

---

## 41. Sandbox池化讨论

**用户**：还需要注意在sandbox本身可以是一个资源池，动态关联使用某一个具体的sandbox

**Coding Agent**：好，这引入了Sandbox池化和动态调度概念。

新的架构理解：SandboxPool（资源池）→ 动态调度 → AIAgent/AgentRuntime。

下一个问题：Sandbox实例的生命周期归属

**选项A：独立SandboxPool CRD管理**
**选项B：AIAgent/AgentRuntime创建时触发Sandbox创建**
**选项C：两者并存**

---

## 42. 探索agent-sandbox项目

**用户**：agent-sandbox项目已经有sandbox和资源池的管理了，所以如何利用好现有的sandbox CRD资源，以及把刚才说得ai agent和sandbox包括的运行关系考虑进去

**Coding Agent**：需要先了解agent-sandbox项目的现有Sandbox CRD设计。我探索了agent-sandbox子目录，这是agent-sandbox的源代码。

[Coding Agent探索了agent-sandbox项目的API定义，发现了以下核心CRD：]

- Sandbox：沙箱实例（Pod）
- SandboxTemplate：沙箱模板，定义Pod规格、网络策略
- SandboxWarmPool：预热池，维护预热的Sandbox实例
- SandboxClaim：沙箱申领，获取Sandbox实例

SandboxClaim的WarmPool策略：
- none：不使用WarmPool，总是创建新Sandbox
- default：从所有匹配的WarmPool中选择（默认）
- 具体池名：只从指定的WarmPool获取

与AI Agent架构的结合点：基于现有设计，AIAgent/AgentRuntime可以通过SandboxClaim来获取Sandbox。

下一个问题：如何将AgentRuntime与Sandbox关联

**选项A：AgentRuntime直接创建SandboxClaim**
**选项B：通过Harness引用已存在的Sandbox/SandboxClaim**
**选项C：嵌入式Sandbox（AgentRuntime Pod本身就是Sandbox）**

---

## 43. Sandbox关联方式决策

**用户**：选项B：通过Harness引用已存在的Sandbox/SandboxClaim；要支持独立sandbox模式和嵌入sandbox模式

**Coding Agent**：好，确定。

通过Harness引用已存在的Sandbox/SandboxClaim，支持两种模式：
- 独立Sandbox
- 嵌入式Sandbox

下一个问题：Harness中Sandbox类型的配置结构

这两种模式是互斥的（一个Harness只能选择一种），还是可以同时配置？

---

## 44. Sandbox模式互斥决策

**用户**：这两种模式是互斥的（一个Harness只能选择一种）

**Coding Agent**：好，确定。一个Harness只能选择一种Sandbox模式。

下一个问题：嵌入式模式下AgentRuntime与SandboxTemplate的关系

**选项A：AgentRuntime引用SandboxTemplate，Pod规格由SandboxTemplate主导**
**选项B：AgentRuntime定义Pod规格，SandboxTemplate只提供安全策略**

---

## 45. SandboxTemplate引用决策

**用户**：选项A：AgentRuntime引用SandboxTemplate，Pod规格由SandboxTemplate主导

**Coding Agent**：好，确定。

嵌入式模式下：SandboxTemplate定义Pod基础规格，AgentRuntime的Handler和Framework容器叠加到Pod。

下一个问题：容器叠加合并规则

**选项A：追加模式**
**选项B：替换模式**
**选项C：用户指定合并策略**

---

## 46. 容器合并策略决策

**用户**：选项A：追加模式

**Coding Agent**：好，确定。

追加模式：SandboxTemplate容器 + AgentRuntime容器（Handler、Framework）。

已确认设计汇总（更新）。

下一个问题：AgentRuntime CRD完整结构

是否还需要其他字段？比如：
- replicas（副本数）
- resources（资源限制）
- nodeSelector/affinity（节点调度）

---

## 47. AgentRuntime CRD完整示例确认

**用户**：可以加入这些字段作为示例，初始设计就基本上可以了

**Coding Agent**：给出AgentRuntime CRD完整示例、AIAgent CRD完整示例、Harness CRD完整示例。

初始设计完成确认：是否还有其他问题需要讨论？

---

## 48. 文档记录请求

**用户**：把整个讨论和设计写入到文档中，中英文各提供一份，把每一个设计的考虑因素都详细说明清楚，并总结，通过这样的设计，能够达到什么样的目标

**Coding Agent**：创建完整的设计文档，中英文各一份。

文档已创建：
- docs/design/ai-agent-architecture-design-CN.md（中文）
- docs/design/ai-agent-architecture-design-EN.md（英文）

---

## 49. 对话记录请求

**用户**：把整个对话的过程也记录下来，包括用户和coding agent的回答，作为原始的prompt的记录。用中英文各存放在一个文件中

**Coding Agent**：创建对话记录文件。

---

## 50. 文件重命名请求

**用户**：把设计文档的文件名改短一些ai-agent-architecture-design 改成ai-agent-design

**Coding Agent**：文件已重命名。

| 原文件名 | 新文件名 |
|----------|----------|
| ai-agent-architecture-design-CN.md | ai-agent-design-CN.md |
| ai-agent-architecture-design-EN.md | ai-agent-design-EN.md |

---

## 51. 对话文档分离请求

**用户**：把conversation的文档移动到docs/conversation子目录，和design子目录分开。并且文件名ai-agent-conversation缩短为conversation

**Coding Agent**：已完成。

最终文件结构：
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

## 52. MCP Registry模式讨论

**用户**：Harness里面，MCP的示例是否应该只是配置mcp server的registry，已经允许发行和不允许发现的mcp server，要不然只能具体配置mcp server，harness里面无法枚举所有的mcp server，handler也没有办法做标准化处理

**Coding Agent**：好观点。当前设计的问题是MCP server无法枚举，Handler也无法标准化处理具体server。

我来更新设计文档，改为配置MCP Registry模式。

**MCP Harness Registry配置结构**：

```yaml
spec:
  type: mcp
  mcp:
    registry:
      endpoint: https://mcp-registry.example.com
      authSecretRef: mcp-registry-token
    allowedServers:     # 允许发现的MCP Server白名单
      - github
      - browser
      - filesystem
    deniedServers:      # 禁止发现的MCP Server黑名单
      - dangerous-tool
    discoveryPolicy: allowlist  # 发现策略：allowlist | denylist | all
```

**关键设计考虑**：
- MCP Server数量庞大无法枚举
- Harness只配置Registry而非具体Server
- Handler标准化处理Registry连接和Server发现机制
- 具体Server由Agent业务动态决定，通过Registry获取
- 白名单/黑名单策略控制可用Server范围

---

## 53. 刷新对话文档请求

**用户**：把新增加相关对话也刷新到中英文对话文档中

**Coding Agent**：正在更新对话文档，添加MCP Registry模式的讨论内容。

---

## 54. agentConfig增强设计讨论启动

**用户**：现在考虑另外一项增强，就是有一些agent框架在启动agent的时候，需要注入诸如prompt等信息，通信协议a2a，或者agent registry用于能力发现等，这些agent定制化的信息。应该如何增强现有的设计，一步一步交互确定

**Coding Agent**：好，开始新的设计讨论。先理解清楚需求范围。

需要注入的定制化信息：
- Prompt：Agent的提示词/指令
- 通信协议：如A2A（Agent-to-Agent）协议配置
- Agent Registry：用于能力发现

第一个问题：注入时机

**选项A**：AIAgent CRD创建时注入
**选项B**：运行时动态注入
**选项C**：两者并存

---

## 55. 注入时机决策

**用户**：两者并存，这种注入信息采用文件还是配置信息传递给handler会比较合适，是在crd还是专门一个harness来处理这种初始化和或许可能更新的处理

**Coding Agent**：好，确定两者并存模式。

下一个问题：传递方式

两个维度可以组合：
1. 文件 vs 配置信息传递给Handler
2. CRD内嵌 vs 专门Harness处理

展开方案C和D（文件传递）的详细讨论。

---

## 56. 文件传递方案讨论

**用户**：C和D方案分别展开讨论一下

**Coding Agent**：详细展开方案C和D：

**方案C：文件传递 + AIAgent CRD内嵌**
- AIAgent CRD中直接嵌入文件内容
- Controller自动创建ConfigMap存储
- 无法复用，每个Agent独立配置

**方案D：文件传递 + 专门Harness CRD**
- AgentConfig作为独立Harness类型
- AIAgent引用Harness，可覆盖部分配置
- 支持多Agent共享配置模板

对比总结：方案C适用于Agent配置独立，方案D适用于多Agent共享配置模板。

---

## 57. 更抽象的设计思路

**用户**：还是不够抽象，实际上只要定义一个文件传递的方式，具体文件内容由handler决定格式，这样用户根据handler的格式指定相关内容在文件里面，即可把handler启动agent需要的内容传递给agent框架

**Coding Agent**：理解了，这是更抽象的思路，符合Handler模式的设计理念。

核心思路：平台层只定义文件传递机制，具体文件内容由Handler决定格式，用户按照Handler要求提供内容。

下一个问题：文件来源

**选项A**：AIAgent CRD中直接嵌入文件内容
**选项B**：引用外部ConfigMap/Secret
**选项C**：两者并存

---

## 58. 文件来源决策

**用户**：引用外部ConfigMap/Secret

**Coding Agent**：好，确定采用引用外部ConfigMap/Secret方式。

下一个问题：挂载路径规范

**选项A**：统一挂载路径
**选项B**：用户指定挂载路径
**选项C**：统一路径 + 子目录命名规范

---

## 59. 挂载路径决策

**用户**：选项A：统一挂载路径

**Coding Agent**：好，确定采用统一挂载路径。

- 统一挂载路径：`/etc/agent-config/`
- 子目录按引用name创建
- Handler知道去这个路径读取所有配置文件

下一个问题：运行时更新机制

**选项A**：Handler主动监听文件变更
**选项B**：平台通知Handler
**选项C**：重启Pod

---

## 60. 更新机制决策

**用户**：选项A：Handler主动监听文件变更

**Coding Agent**：好，确定采用Handler主动监听文件变更。

- Handler使用fsnotify或轮询监听`/etc/agent-config/`目录
- 文件变更时，Handler重新加载配置并更新Agent
- Handler自行决定更新策略

下一个问题：多配置文件的优先级

**选项A**：按引用顺序优先
**选项B**：用户显式指定优先级
**选项C**：不处理，由Handler决定

---

## 61. 文件冲突处理决策

**用户**：文件应该做到不同名，配置项由handler处理，理论上handler定义这些配置文件内容时候，避免同名

**Coding Agent**：好，确定。

- Handler负责定义配置文件的命名规范，避免同名
- 用户按Handler要求准备不同名称的文件
- 平台层不处理文件冲突，只负责挂载

下一个问题：ConfigMap/Secret的声明方式

**选项A**：在AIAgent CRD中声明
**选项B**：在AgentRuntime CRD中声明公共配置，AIAgent可追加
**选项C**：只在AIAgent中声明，Runtime不涉及

---

## 62. 声明方式决策

**用户**：选项B：在AgentRuntime CRD中声明公共配置，AIAgent可追加。因为有些配置是针对所有这类agent是一样的，还有一些配置是AI agent特有的。现在给的只是示例

**Coding Agent**：好，确定。

- AgentRuntime CRD声明公共配置（针对所有同类Agent）
- AIAgent CRD追加Agent专属配置
- 类似Harness的继承模式

下一个问题：AIAgent追加配置时的覆盖行为

**选项A**：AIAgent覆盖Runtime配置
**选项B**：合并挂载（两者都挂载）
**选项C**：用户显式指定覆盖或追加

---

## 63. 覆盖行为决策

**用户**：选项B：合并挂载（两者都挂载）。由handler确定处理逻辑，具有最大的灵活性

**Coding Agent**：好，确定。

- Runtime公共配置挂载到`/etc/agent-config/runtime/`
- AIAgent配置挂载到`/etc/agent-config/agent/`
- Handler自行决定合并逻辑，具有最大灵活性

下一个问题：AgentRuntime级别的agentFiles是否也支持动态更新

**选项A**：支持动态更新
**选项B**：不支持，创建后不可变
**选项C**：支持但需要用户确认

---

## 64. Runtime动态更新决策

**用户**：选项A：支持动态更新。由handler决定更新的方式

**Coding Agent**：好，确定。

- AgentRuntime的agentFiles支持动态更新
- Handler监听文件变更并决定更新方式

下一个问题：ConfigMap/Secret的引用范围

**选项A**：只能引用同Namespace的ConfigMap/Secret
**选项B**：支持跨Namespace引用
**选项C**：两者并存，默认同Namespace，可选指定

---

## 65. 引用范围决策

**用户**：选项A：只能引用同Namespace的ConfigMap/Secret。考虑多租户支持，跨namespace引用，只有等实际有用例的时候再考虑

**Coding Agent**：好，确定。

- 只能引用同Namespace的ConfigMap/Secret
- 符合多租户隔离原则
- 跨Namespace引用待实际用例需求时再考虑

agentFiles设计汇总确认。

---

## 66. CRD结构示例确认

**Coding Agent**：给出agentFiles的CRD结构示例和Pod挂载结构。

**用户**：结构基本符合预期，注意harness和配置的区别。harness是围绕ai agent可以构筑的外置平台工程能力，外置能力比如可观察性，安全，流量治理等可以根据agent id，进行细粒度的处理；而配置是不涉及平台工程能力，主要是ai agent/handler/framework需要的配置信息

**Coding Agent**：好，这是重要的概念澄清。

Harness vs agentFiles区分：
- Harness：平台工程能力，平台层负责管理和提供
- agentFiles：业务配置，Handler决定格式和用途

下一个问题：命名确认

**选项A**：保持agentFiles命名
**选项B**：使用agentConfig命名
**选项C**：使用其他命名

---

## 67. 命名决策

**用户**：选项B：使用agentConfig命名

**Coding Agent**：好，确定。使用agentConfig命名。

给出最终CRD结构示例，包含harness和agentConfig的区分。

agentConfig设计汇总确认。

---

## 68. 文档更新请求

**用户**：更新中英文设计文档，特别是把harness和config的不同目的和设计考量要写清楚。同时把中英文的conversation文档也更新

**Coding Agent**：正在更新设计文档和对话文档，添加Harness与agentConfig的概念区分和agentConfig设计章节。

**Coding Agent**：正在创建对话记录文件。

---

## 69. 章节结构调整请求

**用户**：7. Sandbox集成设计 合并到AgentRuntime设计，作为agent runtime的一个小节。agentConfig设计合并到AI Agent的设计里面，作为AIAgent设计的一个小节。章节Harness与agentConfig的概念区分以及章节Harness配置传递机制，都作为Harness设计的下面的小节，注意逻辑顺序。

**Coding Agent**：好的，我来调整章节结构：
- Sandbox集成设计作为AgentRuntime设计的3.4小节
- agentConfig设计作为AIAgent设计的4.6小节
- Harness与agentConfig概念区分作为Harness设计的5.1小节
- Harness配置传递机制作为Harness设计的5.8小节

同时需要调整后续章节编号和CRD示例中的agentConfig字段。

---

## 70. 删除类比关系表格

**用户**：把中英文的这一节去掉：**Analogy Relationship**:

**Coding Agent**：已从中英文文档中删除类比关系表格。

---

## 71. Agent Framework运行模式设计请求

**用户**：AgentRuntime Design增加支持 agent framework的几种运行模式，long running服务，事件触发按需运行，对外表现server模式(监听端口)和client模式(类似聊天软件的客户端，比如OpenClaw，WeChat的weixin-claw)

**Coding Agent**：好的，我来在AgentRuntime Design章节中增加Agent Framework运行模式：

#### 生命周期模式
- Long Running：长期运行的服务，持续提供服务能力
- Event-triggered：事件触发按需运行，完成任务后终止

#### 通信模式
- Server模式：监听端口，对外提供服务
- Client模式：主动连接外部服务，类似聊天客户端

同时调整后续章节编号：
- 3.3 Pod容器配置 → 3.4 Pod容器配置
- 3.4 Sandbox集成设计 → 3.5 Sandbox集成设计
- 3.5 CRD结构示例 → 3.6 CRD结构示例

---

## 72. 更新Client模式示例图示

**用户**：Client模式示例应该修改成：
```
AgentRuntime Pod
│
└── Agent Framework
    └── AIAgent (OpenClaw / weixin-claw) ──► 连接外部平台服务
                                              │
                                              ▼
                                        WhatsApp / Discord / WeChat...
```

**Coding Agent**：已修改完成。Client模式示例图示已更新：
- AIAgent标注为具体框架类型（OpenClaw / weixin-claw）
- 连接目标改为WhatsApp / Discord / WeChat等聊天平台

---

## 73. 对话记录更新请求

**用户**：把对话记录更新到conversation相应文件

**Coding Agent**：正在更新对话记录文件，追加本次对话内容。

---

## 74. 资源效率考虑章节请求

**用户**：在AgentRuntime中增加一节关于资源效率的考虑，对于很多AIAgent来说，平常非常空闲，只有任务来了的时候忙，有的AIAgent是短任务运行，有的是长任务运行，因此通过轻量级的agenthandler，以各种方式运行agent framework，在多ai agent中共用一个pod的资源进行资源共享，提升资源利用效率。

**Coding Agent**：好的，我来在AgentRuntime Design章节中增加资源效率考虑小节（3.4），包括：

#### 资源使用特征
- 空闲时间长：Agent大部分时间处于空闲状态
- 任务突发性：任务到来时资源使用突增
- 任务时长差异：短任务和长任务并存
- 资源需求波动：不同任务资源需求差异大

#### 资源共享策略
- 多Agent共享Pod资源：空闲Agent共享基础设施开销
- 按任务类型分组：短任务组、长任务组、混合组

#### 资源效率量化分析
对比传统模式与共享模式的效率差异，共享模式资源利用率可达60%~80%

#### 设计要点
- 轻量级Agent Handler
- Framework进程共享
- 动态资源调度
- Pod级别资源配额

同时调整后续章节编号：
- 3.4 Pod容器配置 → 3.5 Pod容器配置
- 3.5 Sandbox集成设计 → 3.6 Sandbox集成设计
- 3.6 CRD结构示例 → 3.7 CRD结构示例

---

## 75. 修改资源效率章节

**用户**：#### 3.4.3 资源效率量化分析 删除，然后另外要考虑单Agent Framework 多 AI Agent和 每个Agent Framework一个AI Agent，但是通过多Agent Framework多进程实现多agent这两种一个AgentRuntime支持多AI agent模式可以提高资源利用效率。

**Coding Agent**：好的，我来修改：

1. 删除 3.4.3 资源效率量化分析章节
2. 修改 3.4.2 资源共享策略，改为两种AgentRuntime支持多AI Agent的模式：

**模式一：单Agent Framework多AI Agent**
- 一个Agent Framework进程承载多个AI Agent
- 进程级别资源共享：内存、网络连接、运行时环境
- Framework内部实现Agent调度和资源分配

**模式二：多Agent Framework进程多AI Agent**
- 每个Agent Framework进程对应一个AI Agent
- 通过多进程实现多Agent，共享Pod基础设施和资源配额
- 进程级隔离，单Agent故障不影响其他Agent

增加对比分析表，说明两种模式在进程数量、资源共享粒度、隔离强度、资源效率等方面的差异。

---

## 76. 修改描述

**用户**：把这句话改一下：Agent框架原生支持多Agent（如CrewAI、ADK多Agent） -> Agent框架原生支持单进程多Agent

**Coding Agent**：已修改完成。

---

## 77. 删除描述

**用户**：这句话删除：Agent之间协作紧密，需要共享上下文

**Coding Agent**：已删除。中英文文档都已移除这句话。

---

## 78. 增加模型作为平台服务能力

**用户**：在Harness中增加模型作为平台服务能力

**Coding Agent**：已在Harness设计中增加model类型：

**标准类型列表新增**：
- model：模型服务，LLM模型接入配置

**Model类型Spec结构**：
```yaml
spec:
  type: model
  model:
    provider: openai          # 模型提供商
    endpoint: https://api.openai.com/v1
    authSecretRef: openai-api-key
    models:                   # 可用模型列表
      - name: gpt-4
        allowed: true
        rateLimit: 100
      - name: gpt-3.5-turbo
        allowed: true
      - name: o1-preview
        allowed: false
    defaultModel: gpt-4
```

**考虑因素**：
- 模型服务是Agent核心依赖，需要统一配置管理
- 平台层提供模型接入能力，Handler无需处理不同提供商细节
- 支持多模型配置和访问控制
- 认证信息通过Secret管理

同时更新了：
- AgentRuntime CRD示例中添加model harness引用
- Harness CRD示例中添加model类型示例
- 章节编号调整（5.3.1 Model类型设计，5.3.2 MCP类型，5.3.3其他类型）

---

## 79. 删除注释并改用开源模型示例

**用户**：删除这个注释 # Model provider: openai | anthropic | azure | local | custom，另外模型使用开源模型为例子，比如deepseek, qwen

**Coding Agent**：已完成修改：

1. **删除注释**：移除了provider字段的注释说明

2. **改用开源模型示例**：
   - provider: deepseek
   - endpoint: https://api.deepseek.com/v1
   - models: deepseek-chat, deepseek-coder, qwen-turbo, qwen-max
   - defaultModel: deepseek-chat

3. **更新所有相关位置**：
   - Spec结构示例（5.3.1章节）
   - Harness CRD示例（5.9章节）
   - AgentRuntime harness引用
   - metadata.name: model-openai-default → model-deepseek-default

---

## 80. Harness章节标题修改

**用户**："5. Harness Design"这一章节的标题修改为 "5. Harness Design(WIP and TBD)"

**Coding Agent**：已完成修改：
- 中文：`## 5. Harness设计` → `## 5. Harness设计（WIP and TBD）`
- 英文：`## 5. Harness Design` → `## 5. Harness Design (WIP and TBD)`

---

## 81. 在概述前面增加问题和挑战章节

**用户**：在文档概述的最前面，增加### 1.0 问题和挑战

#### 1.0.1 AI Agent的资源利用效率问题

AI Agent具有一些新的资源使用特征：
- 空闲时间长
- 任务突发性
- 任务时长差异
- 资源需求波动

AI Agent和执行环境存在多样化的考虑：合并或分离。

当Kubernetes集群运行规模数量AI Agent时，如何有效地提升集群的资源利用效率，是一个共性的问题。

#### 1.0.2 AI Agent技术快速迭代，平台工程跟不上AI Agent框架发展

从Langchain到Manus到OpenClaw、Hermes，技术框架不断演进。CNCF/Kubernetes的平台工程还是传统的Pod、微服务、服务网格基础上构建。需要解决对AI Agent粒度的感知问题。

**Coding Agent**：已在文档概述最前面增加"1.0 问题和挑战"章节，包含：
- 1.0.1 AI Agent的资源利用效率问题
- 1.0.2 AI Agent技术快速迭代，平台工程跟不上AI Agent框架发展

章节结构变为：
- ## 1. 概述
  - ### 1.0 问题和挑战（新增）
    - #### 1.0.1 资源利用效率问题
    - #### 1.0.2 技术迭代问题
  - ### 1.1 目的
  - ### 1.2 核心目标

---

## 82. 修改目的章节内容

**用户**：修改"1.1 目的"的内容：
- Agent框架列表改为：LangChain、Sematic Kernel、OpenClaw、Hermes等
- 添加"将来未知的Agent框架"
- 在脚手架能力列表开头添加"Model"
- 将"AI Agent ID"改为"AI Agent ID/Name"

**Coding Agent**：已完成修改：

原文：
- 框架：LangChain、ADK、OpenClaw、CrewAI、Hermes等
- 能力：CLI Tools、MCP、Skills...
- 连接：AI Agent ID

修改后：
- 框架：LangChain、Sematic Kernel、OpenClaw、Hermes等 + 将来未知的Agent框架
- 能力：Model、MCP、Skills...
- 连接：AI Agent ID/Name

---

## 83. 删除核心目标章节

**用户**：### 1.2 核心目标 这一节去掉

**Coding Agent**：已删除"### 1.2 核心目标"章节。章节结构变为：
- ## 1. 概述
  - ### 1.0 问题和挑战
  - ### 1.1 目的
- ## 2. 核心对象定义

---

## 84. 添加免责声明注解

**用户**：在文档最后增加一个注解：The opinions expressed in this article do not reflect the view of the author's affiliation

**Coding Agent**：已在文档最后添加注解：
- 中文：**注：本文所表达的观点不代表作者所属机构的立场。**
- 英文：**Note: The opinions expressed in this article do not reflect the view of the author's affiliation.**

---

## 85. 修改1.0章节标题并添加典型场景

**用户**：
1. ### 1.0的标题更改为 ### 1.0 场景和挑战
2. 增加 #### 1.0.1 典型场景 小节，内容是关于Alice的AI Agent创业公司通过WeChat接入的场景描述

**Coding Agent**：已完成修改：

1. **标题修改**：
   - 中文：### 1.0 问题和挑战 → ### 1.0 场景和挑战
   - 英文：### 1.0 Problems and Challenges → ### 1.0 Scenarios and Challenges

2. **新增典型场景小节（1.0.1）**：
   - WeChat开放AI Agent接口，用户可扫码添加AI Agent为好友
   - Alice作为一人创业公司开发生活助手AI Agent
   - 通过WeChat接入降低推广和运维成本
   - 面临资源成本问题：单进程多Agent、Agent/Sandbox分离复用
   - 用户活跃度差异大，需要动态consolidate、扩缩容
   - 需要AI Agent粒度的平台工程

3. **章节编号调整**：
   - 原1.0.1 → 1.0.2（资源利用效率问题）
   - 原1.0.2 → 1.0.3（技术迭代问题）

---

## 86. 修改英文文档典型场景中的表述

**用户**：修改英文文档中的三处表述：
1. "WeChat recently opened an AI Agent interface similar to OpenClaw" → "WeChat recently opened an weixin-claw plugin for AI Agent like Openclaw or any other AI Agents to be accessible in WeChat"
2. "One user exclusively occupying" → "One AI Agent exclusively occupying"
3. "If users grow to millions or tens of millions" → "If AI Agents grow to millions or tens of millions"

**Coding Agent**：已完成修改：

1. **weixin-claw插件描述**：更准确地描述WeChat开放的weixin-claw插件，使得类似OpenClaw或其他AI Agent可以接入WeChat
2. **资源占用主体修正**：从"user独占"改为"AI Agent独占"，更准确地描述资源分配粒度
3. **规模增长主体修正**：从"users增长"改为"AI Agents增长"，更准确地描述业务规模

---

## 87. 添加脚注

**用户**：在文档最后添加一个脚注，在其他文档中引用scenario的场景，对文章作者进行credit，以比较通俗习惯的说明添加脚注

**Coding Agent**：已在文档最后添加脚注：
- 中文：**脚注**：如在其他文档或文章中引用本文"1.0.1 典型场景"章节的案例描述，请注明出处及本文作者。
- 英文：**Footnote**: When referencing the "1.0.1 Typical Scenario" section in other documents or articles, please cite the source and credit the author of this article.

---

## 88. 英文文档手动更新后同步到中文文档

**用户**：ai-agent-abstraction-EN.md文档手工刷新了，请重新加载，把ai-agent-abstraction-EN.md英文文档修改了的地方，同步到ai-agent-abstraction-CN.md

**Coding Agent**：已同步英文文档的修改到中文文档：

1. **章节编号调整**：
   - 1.0 场景和挑战 → 1.1 场景和挑战
   - 1.0.1 典型场景 → 1.1.1 典型场景
   - 1.0.2 → 1.1.2
   - 1.0.3 → 1.1.3
   - 1.1 目的 → 1.2 设计目的

2. **新增内容**：在1.2设计目的章节末尾添加了关于资源利用效率提升的功能描述：
   "为了提升资源利用效率，AI Agent抽象应能够支持AI Agent装箱整合（bin pack consolidation）、AI Agent迁移、Pod/Node扩缩容、Pod调整大小、Sandbox复用/休眠/调整大小等功能的实现。"

3. **引用更新**：将1.1.3章节中的引用从"1.0.1"更新为"1.1.1"

4. **脚注更新**：脚注中的章节引用从"1.0.1"更新为"1.1.1"

---

## 89. 同步英文文档AgentRuntime章节到中文

**用户**：这些英文刷新内容也要同步到中文版本3. AgentRuntime Design章节

**Coding Agent**：已同步AgentRuntime章节的修改到中文文档：

1. **段落更新**：添加了"AgentRuntime和AIAgent CRD的生命周期由AgentRuntime Controller统一管理，该Controller由平台层提供。"

2. **结构调整**：
   - 移除了"问题：如何避免为每种Agent框架开发独立Controller？"的问题格式
   - 移除了"决策：采用Agent Handler模式。"的决策格式
   - 移除了"平台层Controller"的单独定义

3. **新增Agent Framework定义**：添加了Agent Framework的定义项
   "Agent Framework：如LangChain、Sematic Kernel、OpenClaw、Hermes等运行AI Agent的框架。"

4. **Agent Handler描述更新**：从"负责具体框架的启动、配置转换、Agent管理"更新为"负责具体框架的启动、配置转换和AI Agent生命周期管理。"

---

## 90. 同步英文文档全面更新到中文

**用户**：ai-agent-abstraction-EN.md文档手工刷新了，请重新加载，把ai-agent-abstraction-EN.md英文文档修改了的地方，同步到ai-agent-abstraction-CN.md

**Coding Agent**：已全面同步英文文档的所有修改到中文文档：

**1.1.1 典型场景章节修改**：
- 更新WeChat插件描述为通用AI Agent插件
- 更新"Web服务安全治理和运维"→"Web服务治理、安全和运维"
- 更新"AI Agent和Sandbox分离"→"AI Agent和执行环境Sandbox分离"
- 添加日常高峰低谷波动管理描述
- 更新扩缩容术语（scaling out/in）

**1.1.2章节**：
- 添加"或混合模式"到执行环境考虑

**1.2设计目的章节**：
- 添加"ANY"强调词
- 添加ADK到框架列表
- 重写资源效率提升的技术描述

**3. AgentRuntime章节**：
- 更新Controller描述（framework agnostic）
- 添加TBD：Agent Handler不应参与运营流量
- 更新进程映射模式的进程图表（process而非container）
- 更新多Agent实例描述（相同类型或不同类型）
- 添加Agent Framework进程只支持一个对外可见Agent的考虑

**3.4资源效率章节重构**：
- 移除原3.4.1资源使用特征独立章节
- 合并到3.4.1资源共享策略
- 3.4.2改为设计要点
- 对比表移除隔离强度列

**3.5/3.6.3章节**：
- 添加TBD：Agent Handler管理机制设计
- 添加TBD：Sandbox复用时AI Agent无缝切换

**新增3.7章节**：
- Agent标识管理：CRD ID/Name与框架内部UUID映射

**3.8 CRD示例更新**：
- 移除resources字段
- 更新模型名称为model-deepseek-default

**4. AIAgent章节修改**：
- 4.1开头改为"对于资源效率至关重要"
- 4.4迁移触发条件添加整合、修改示例格式
- 4.6.2考虑因素改为"受控技能集"
- 4.7 CRD示例改为agent-skill-set

**5. Harness章节修改**：
- 添加Handler作为中介的段落说明
- 5.1.1示例更新为GAIE Gateway等
- 5.1.2 Harness描述更新

**7.总结章节**：
- 移除Pod/Node类比
- 添加资源效率目标

---

## 91. 同步英文文档缩减内容到中文

**用户**：ai-agent-abstraction-EN.md文档手工进行缩减内容和刷新了，请重新加载，把ai-agent-abstraction-EN.md英文文档修改了的地方，同步到ai-agent-abstraction-CN.md

**Coding Agent**：已同步英文文档的缩减修改到中文文档：

**第5章Harness大幅精简**：
- 合并5.2.1/5.2.2/5.2.3为简化版5.2.1设计考虑
- 5.2.4标准类型列表改为5.2.2标准能力支持，移除cli-tools和gateway
- 移除5.3.1/5.3.2/5.3.3详细设计，简化为Model类型示例
- 移除5.5配置优先级、5.6能力校验章节
- 合并5.7到5.4.2结尾
- 简化5.8配置传递机制：移除设计考虑表格、ConfigMap大小约束
- 完全移除5.9 CRD结构示例章节
- 章节编号调整：5.5→原5.8，5.5.1/5.5.2简化

**第6章修改**：
- 6.3效果描述添加binpack、auto scaling

**移除脚注**：
- 删除文档末尾的脚注说明