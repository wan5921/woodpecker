// =============================================================================
// Feature 1: Matrix Generation — Extended CI Config & Generation Pseudocode
// =============================================================================
//
// 现有架构中的 matrix 解析 (pipeline/frontend/yaml/matrix/matrix.go) 已经在 YAML
// 级别解析 matrix 块并生成 []Axis。但 .woodpecker.yml 的 Workflow 结构体本身并
// 不直接承载 matrix 字段——matrix 是在 builder.go 中通过 ParseString 从原始 YAML
// 数据中单独提取的。
//
// 本设计将 matrix 正式纳入 Workflow 结构体，并在 builder 层统一生成并行流水线。

package design

import (
	"fmt"
	"strings"

	"go.woodpecker-ci.org/woodpecker/v3/pipeline/frontend/yaml/constraint"
)

// ── 扩展后的 Workflow 结构体 ─────────────────────────────────────────────────

// Workflow 是增强版的流水线配置，matrix 字段正式成为其一部分。
// 对比原版 (pipeline/frontend/yaml/types/workflow.go):
//   - 新增 Matrix 字段，支持 go: [1.21, 1.22, 1.23] 这种笛卡尔积写法
//   - 新增 MatrixInclude 字段，支持显式列举特定组合
type Workflow struct {
	When      constraint.When      `yaml:"when,omitempty"`
	Workspace Workspace            `yaml:"workspace,omitempty"`
	Clone     ContainerList        `yaml:"clone,omitempty"`
	Steps     ContainerList        `yaml:"steps,omitempty"`
	Services  ContainerList        `yaml:"services,omitempty"`
	Labels    map[string]string    `yaml:"labels,omitempty"`
	DependsOn constraint.DependsOn `yaml:"depends_on,omitempty"`
	SkipClone bool                 `yaml:"skip_clone,omitempty"`
	RunsOn    []string             `yaml:"runs_on,omitempty"`

	// 矩阵生成（新增）
	Matrix        MatrixDefinition `yaml:"matrix,omitempty"`
	MatrixInclude []AxisExplicit   `yaml:"matrix_include,omitempty"`
}

// Workspace 定义工作空间（与原版一致）
type Workspace struct {
	Base string
	Path string
}

// ContainerList 是容器/步骤列表（与原版一致）
type ContainerList []Container

// Container 定义单个容器/步骤（与原版一致）
type Container struct {
	Name        string             `yaml:"name,omitempty"`
	Image       string             `yaml:"image,omitempty"`
	Commands    StringOrSlice      `yaml:"commands,omitempty"`
	Settings    map[string]any     `yaml:"settings,omitempty"`
	Environment map[string]any     `yaml:"environment,omitempty"`
	DependsOn   constraint.DependsOn `yaml:"depends_on,omitempty"`
	When        constraint.When      `yaml:"when,omitempty"`
	Failure     string               `yaml:"failure,omitempty"`
}

// StringOrSlice 表示可以是单个字符串或字符串数组（与原版一致）
type StringOrSlice []string

// ── 矩阵定义 ─────────────────────────────────────────────────────────────────

// MatrixDefinition 定义笛卡尔积矩阵。
//
// YAML 写法:
//
//	matrix:
//	  go: [1.21, 1.22, 1.23]
//	  os: [linux, darwin]
//
// 这将生成 3*2=6 个并行流水线。
type MatrixDefinition map[string][]string

// AxisExplicit 通过 include 显式定义一组矩阵轴，不走笛卡尔积。
//
// YAML 写法:
//
//	matrix:
//	  include:
//	    - go: 1.21
//	      os: linux
//	    - go: 1.22
//	      os: darwin
type AxisExplicit map[string]string

// MatrixAxis 表示单个矩阵组合（即一个具体的并行流水线实例）
type MatrixAxis map[string]string

// ── 矩阵生成器 ───────────────────────────────────────────────────────────────

// MatrixGenerator 根据 Workflow 中的 matrix 定义生成并行流水线列表。
type MatrixGenerator struct {
	MaxCombinations int // 最大组合数限制，默认 25
	MaxDimensions   int // 最大维度数限制，默认 10
}

// GeneratedPipeline 表示矩阵生成后的单个并行流水线。
type GeneratedPipeline struct {
	Name        string            // 流水线名称，例如 "test (go=1.21, os=linux)"
	AxisID      int               // 轴编号，从 1 开始
	Environ     map[string]string // 矩阵变量注入为环境变量，如 CI_MATRIX_GO=1.21
	OriginalYAML string           // 经过模板替换后的 YAML
}

// Generate 从 Workflow 生成并行流水线列表。
//
// 算法:
//  1. 若设置了 matrix_include，直接使用显式定义的组合
//  2. 否则对 matrix 做笛卡尔积展开
//  3. 对每个组合，将矩阵变量注入 YAML 模板，替换 ${CI_MATRIX_GO} 等占位符
func (g *MatrixGenerator) Generate(w *Workflow) ([]GeneratedPipeline, error) {
	var axes []MatrixAxis

	// 优先使用显式 include
	if len(w.MatrixInclude) > 0 {
		for _, inc := range w.MatrixInclude {
			axes = append(axes, MatrixAxis(inc))
		}
	} else if len(w.Matrix) > 0 {
		// 笛卡尔积展开
		var err error
		axes, err = g.cartesianProduct(w.Matrix)
		if err != nil {
			return nil, err
		}
	}

	if len(axes) == 0 {
		// 无矩阵定义，返回单个流水线
		return []GeneratedPipeline{{
			Name:    "default",
			AxisID:  0,
			Environ: nil,
		}}, nil
	}

	var pipelines []GeneratedPipeline
	for i, axis := range axes {
		pipeline := GeneratedPipeline{
			Name:    g.buildPipelineName(axis, i+1),
			AxisID:  i + 1,
			Environ: g.toCIEnviron(axis),
		}
		pipelines = append(pipelines, pipeline)
	}

	return pipelines, nil
}

// cartesianProduct 计算笛卡尔积。
//
// 例如 matrix: {go: [1.21, 1.22], os: [linux, darwin]}
// 返回:
//
//	[{go: 1.21, os: linux}, {go: 1.21, os: darwin},
//	 {go: 1.22, os: linux}, {go: 1.22, os: darwin}]
func (g *MatrixGenerator) cartesianProduct(m MatrixDefinition) ([]MatrixAxis, error) {
	if g.MaxCombinations == 0 {
		g.MaxCombinations = 25
	}
	if g.MaxDimensions == 0 {
		g.MaxDimensions = 10
	}

	dimKeys := make([]string, 0, len(m))
	for k := range m {
		dimKeys = append(dimKeys, k)
	}

	if len(dimKeys) > g.MaxDimensions {
		return nil, fmt.Errorf("matrix: too many dimensions (%d), max is %d", len(dimKeys), g.MaxDimensions)
	}

	perm := 1
	for _, v := range m {
		perm *= len(v)
	}

	if perm > g.MaxCombinations {
		return nil, fmt.Errorf("matrix: too many combinations (%d), max is %d", perm, g.MaxCombinations)
	}

	var axes []MatrixAxis
	for p := 0; p < perm; p++ {
		axis := MatrixAxis{}
		decrease := perm
		for _, key := range dimKeys {
			elems := m[key]
			decrease /= len(elems)
			elem := p / decrease % len(elems)
			axis[key] = elems[elem]
		}
		axes = append(axes, axis)
	}

	return axes, nil
}

// buildPipelineName 构建流水线展示名称。
// 例如 "test (go=1.21, os=linux)"。
func (g *MatrixGenerator) buildPipelineName(axis MatrixAxis, id int) string {
	parts := make([]string, 0, len(axis))
	for k, v := range axis {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return fmt.Sprintf("#%d (%s)", id, strings.Join(parts, ", "))
}

// toCIEnviron 将矩阵变量转换为 CI_ 前缀环境变量。
// axis {go: "1.21", os: "linux"} ->
// {"CI_MATRIX_GO": "1.21", "CI_MATRIX_OS": "linux"}。
func (g *MatrixGenerator) toCIEnviron(axis MatrixAxis) map[string]string {
	env := make(map[string]string, len(axis))
	for k, v := range axis {
		env["CI_MATRIX_"+strings.ToUpper(k)] = v
	}
	return env
}

// ── Builder 层集成伪代码 ────────────────────────────────────────────────────

// BuildParallelPipelines 是 server 端收到 webhook 后的流水线构建入口。
//
// 流程:
//  1. 解析 .woodpecker.yml → Workflow
//  2. MatrixGenerator.Generate() → []GeneratedPipeline
//  3. 对每个 GeneratedPipeline:
//     a. 执行 envsubst 将 ${CI_MATRIX_GO} 替换为具体值
//     b. 创建 model.Workflow 记录，写入数据库
//     c. 所有 workflow 共享同一个 model.Pipeline 但各自独立调度
func BuildParallelPipelines(rawYAML []byte) ([]GeneratedPipeline, error) {
	// 1. 解析 YAML 到 Workflow 结构体
	// var w Workflow
	// xyaml.Unmarshal(rawYAML, &w)

	// 2. 矩阵生成
	gen := &MatrixGenerator{}
	// pipelines, err := gen.Generate(&w)

	// 3. 为每个组合创建独立的 Workflow
	// for _, p := range pipelines {
	//     substitutedYAML := doEnvsubst(p.OriginalYAML, p.Environ, ...)
	//     wf := &model.Workflow{
	//         PID:    p.AxisID,
	//         Name:   p.Name,
	//         Environ: p.Environ,
	//     }
	//     db.CreateWorkflow(wf)
	// }

	return nil, nil
}

// =============================================================================
// Feature 2: Plugin v1→v2 升级 — 适配器模式
// =============================================================================
//
// Woodpecker 现有两种插件机制：
//   1. "Settings 插件"（v1）: 用户在 yml 中写 settings: {key: value}，compiler 将
//      Settings 展开为 PLUGIN_KEY=value 环境变量注入容器。容器内脚本读完 env 后自行
//      处理业务逻辑。判定依据：Container.IsPlugin() → commands/entrypoint/environment
//      全为空且有 image 字段。
//   2. "Go Plugin / addon 插件"（v1）: 通过 hashicorp/go-plugin 实现的外部进程插件，
//      用于 forge、log 等 server 端组件。核心接口是 Forge interface 和 log.Service。
//
// v2 升级目标：
//   - 统一容器插件和 addon 插件的接口契约
//   - 容器插件增加结构化的输入/输出类型（不再仅靠环境变量传递）
//   - 增加插件生命周期钩子（setup / teardown / validate）
//   - 保持向后兼容：v1 插件无需修改即可在 v2 系统中运行

import (
	"context"
	"encoding/json"
	"os"
)

// ── v1 插件接口（现有） ──────────────────────────────────────────────────────

// PluginV1 是当前 Woodpecker 中 container/settings 插件的隐含接口：
// 没有正式的 Go 接口，编译期通过 Container.IsPlugin() 和 settings.ParamsToEnv()
// 将 map[string]any 展开为 PLUGIN_ 前缀环境变量。
//
// StepConfig 代表用户在 .woodpecker.yml 中写的 step:
//
//	steps:
//	  - name: notify
//	    image: plugins/slack
//	    settings:
//	      webhook: https://...
//	      channel: general
//	      message: "build done"
type StepConfigV1 struct {
	Name     string         `yaml:"name"`
	Image    string         `yaml:"image"`
	Settings map[string]any `yaml:"settings"`
}

// PluginEnvV1 表示编译后的环境变量形式。
// settings: {webhook: "https://...", channel: "general"}
//   → PLUGIN_WEBHOOK=https://...
//   → PLUGIN_CHANNEL=general
type PluginEnvV1 map[string]string

// ── v2 插件接口（新设计） ────────────────────────────────────────────────────

// PluginV2 定义统一的 v2 插件接口。
//
// 关键改进：
//   - 结构化的 Input/Output 类型，替代环境变量拼接
//   - 生命周期钩子：Validate → Setup → Execute → Teardown
//   - 支持插件元数据自描述
//   - 错误处理更丰富
type PluginV2 interface {
	// Metadata 返回插件的元信息，用于 UI 展示和自动文档生成。
	Metadata() PluginMetadata

	// Validate 在流水线编译阶段调用，校验 settings 配置是否合法。
	Validate(input PluginInput) error

	// Setup 在执行前准备资源（如创建临时目录、连接外部服务）。
	Setup(ctx context.Context, input PluginInput) error

	// Execute 执行插件主逻辑。
	Execute(ctx context.Context, input PluginInput) (PluginOutput, error)

	// Teardown 在 Execute 之后调用，无论成功或失败。
	Teardown(ctx context.Context) error
}

// PluginMetadata 插件元数据。
type PluginMetadata struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	InputSchema map[string]any    `json:"input_schema"` // JSON Schema for settings
	Tags        []string          `json:"tags"`
	Labels      map[string]string `json:"labels"`
}

// PluginInput 插件输入——结构化取代环境变量。
type PluginInput struct {
	Params     map[string]any    `json:"params"`     // 原 settings
	Secrets    map[string]string `json:"secrets"`    // 注入的 secrets
	Workspace  string            `json:"workspace"`  // 工作目录路径
	CIEnviron  map[string]string `json:"ci_environ"` // CI 系统环境变量
	StepName   string            `json:"step_name"`  // 当前 step 名称
}

// PluginOutput 插件输出——结构化取代 stdout 解析。
type PluginOutput struct {
	ExitCode int               `json:"exit_code"`
	Stdout   string            `json:"stdout"`
	Metadata map[string]string `json:"metadata"` // 可传递给下游 step 的变量
	Artifacts []string         `json:"artifacts"` // 生成的文件路径列表
}

// ── 适配器：v1→v2 ────────────────────────────────────────────────────────────

// PluginV1Adapter 将 v1 Settings 插件适配到 v2 接口。
//
// 原理：
//  1. Validate: 不做强校验（v1 无法通过接口声明 schema）
//  2. Setup: 将 PluginInput.Params 展开为 PLUGIN_ 环境变量
//  3. Execute: 通过 os/exec 调用 v1 容器镜像，pipe stdin/读取 stdout/stderr
//  4. Teardown: no-op
//
// 这使得所有现有的 v1 插件（plugins/slack, plugins/docker, plugins/git 等）
// 无需任何修改就能在 v2 系统中运行。
type PluginV1Adapter struct {
	image   string
	workDir string
	envV1   PluginEnvV1 // 预计算好的 PLUGIN_ 环境变量
}

// NewPluginV1Adapter 创建 v1→v2 适配器。
func NewPluginV1Adapter(image string) *PluginV1Adapter {
	return &PluginV1Adapter{image: image}
}

func (a *PluginV1Adapter) Metadata() PluginMetadata {
	return PluginMetadata{
		Name:        a.image,
		Version:     "v1",
		Description: "v1 plugin adapted to v2 (no schema available)",
	}
}

func (a *PluginV1Adapter) Validate(input PluginInput) error {
	// v1 插件没有 schema，跳过校验，仅做基本检查
	if a.image == "" {
		return fmt.Errorf("plugin v1 adapter: image is required")
	}
	return nil
}

func (a *PluginV1Adapter) Setup(ctx context.Context, input PluginInput) error {
	// 将结构化的 PluginInput.Params 展开为 PLUGIN_ 前缀环境变量
	a.envV1 = PluginEnvV1{}
	a.workDir = input.Workspace

	// 类似现有 settings/params.go 中的 ParamsToEnv 逻辑
	a.convertParamsToEnv(input.Params)

	// 注入 secrets、CI 环境变量
	for k, v := range input.Secrets {
		a.envV1["PLUGIN_"+strings.ToUpper(k)] = v
	}
	for k, v := range input.CIEnviron {
		a.envV1[k] = v
	}

	return nil
}

func (a *PluginV1Adapter) Execute(ctx context.Context, input PluginInput) (PluginOutput, error) {
	// 将 PLUGIN_ 环境变量以 JSON 形式通过 stdin 泵入容器
	// （v1 容器从环境变量读取，这里也支持 stdin JSON 模式）
	//
	// 伪代码:
	//   cmd := exec.CommandContext(ctx, "docker", "run", a.image)
	//   cmd.Env = a.toEnvSlice()
	//   cmd.Stdin = bytes.NewReader(jsonInput)
	//   var stdout, stderr bytes.Buffer
	//   cmd.Stdout = &stdout
	//   cmd.Stderr = &stderr
	//   err := cmd.Run()
	//
	//   return PluginOutput{
	//       ExitCode: cmd.ProcessState.ExitCode(),
	//       Stdout:   stdout.String(),
	//   }, err

	return PluginOutput{ExitCode: 0, Stdout: "v1 adapter executed"}, nil
}

func (a *PluginV1Adapter) Teardown(ctx context.Context) error {
	// v1 插件无状态，无需清理
	return nil
}

// convertParamsToEnv 将 map[string]any 递归展开为 PLUGIN_ 前缀环境变量。
//
// 规则（与现有 settings/params.go 一致）：
//   - 简单值: key → PLUGIN_KEY=value
//   - 嵌套 map: parent→child → PLUGIN_PARENT_CHILD=value
//   - 数组: key → PLUGIN_KEY=val1,val2,val3
func (a *PluginV1Adapter) convertParamsToEnv(params map[string]any) {
	for k, v := range params {
		a.flattenValue("PLUGIN_"+strings.ToUpper(k), v)
	}
}

func (a *PluginV1Adapter) flattenValue(prefix string, v any) {
	switch val := v.(type) {
	case string:
		a.envV1[prefix] = val
	case bool:
		a.envV1[prefix] = fmt.Sprintf("%t", val)
	case float64:
		a.envV1[prefix] = fmt.Sprintf("%v", val)
	case []any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = fmt.Sprintf("%v", item)
		}
		a.envV1[prefix] = strings.Join(parts, ",")
	case map[string]any:
		for subK, subV := range val {
			a.flattenValue(prefix+"_"+strings.ToUpper(subK), subV)
		}
	default:
		b, _ := json.Marshal(v)
		a.envV1[prefix] = string(b)
	}
}

// toEnvSlice 转换为 os/exec.Cmd.Env 格式（key=value 字符串数组）。
func (a *PluginV1Adapter) toEnvSlice() []string {
	env := os.Environ()
	for k, v := range a.envV1 {
		env = append(env, k+"="+v)
	}
	return env
}

// ── v2 原生插件示例 ──────────────────────────────────────────────────────────

// SlackPluginV2 是 v2 原生插件示例，直接实现 PluginV2 接口。
// 相比 v1 的 plugins/slack，它有明确的类型定义和 schema 声明。
type SlackPluginV2 struct {
	webhookURL string
	channel    string
	message    string
}

type SlackInput struct {
	WebhookURL string `json:"webhook_url"`
	Channel    string `json:"channel"`
	Message    string `json:"message"`
}

func (p *SlackPluginV2) Metadata() PluginMetadata {
	return PluginMetadata{
		Name:    "slack",
		Version: "2.0.0",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"webhook_url": map[string]any{"type": "string", "description": "Slack incoming webhook URL"},
				"channel":     map[string]any{"type": "string", "description": "Target channel"},
				"message":     map[string]any{"type": "string", "description": "Message text"},
			},
			"required": []string{"webhook_url", "message"},
		},
	}
}

func (p *SlackPluginV2) Validate(input PluginInput) error {
	// 使用 input.Params + InputSchema 做 JSON Schema 校验
	return nil
}

func (p *SlackPluginV2) Setup(ctx context.Context, input PluginInput) error {
	p.webhookURL = input.Params["webhook_url"].(string)
	p.channel = input.Params["channel"].(string)
	p.message = input.Params["message"].(string)
	return nil
}

func (p *SlackPluginV2) Execute(ctx context.Context, input PluginInput) (PluginOutput, error) {
	// 实际 HTTP 调用 Slack API
	_ = p.webhookURL
	_ = p.channel
	_ = p.message
	return PluginOutput{ExitCode: 0, Stdout: "notification sent to Slack"}, nil
}

func (p *SlackPluginV2) Teardown(ctx context.Context) error {
	return nil
}

// ── 插件调度器 ───────────────────────────────────────────────────────────────

// PluginDispatcher 根据配置自动选择 v1 适配器或 v2 原生实现。
type PluginDispatcher struct {
	v2Plugins map[string]PluginV2 // 已注册的 v2 原生插件
}

func NewPluginDispatcher() *PluginDispatcher {
	return &PluginDispatcher{
		v2Plugins: make(map[string]PluginV2),
	}
}

func (d *PluginDispatcher) RegisterV2(name string, p PluginV2) {
	d.v2Plugins[name] = p
}

// Resolve 根据插件名解析并返回执行器。
// 优先匹配 v2 原生插件，若找不到则降级为 v1 适配器。
func (d *PluginDispatcher) Resolve(image string) PluginV2 {
	// 1. 检查是否为注册的 v2 插件
	if p, ok := d.v2Plugins[image]; ok {
		return p
	}

	// 2. 降级为 v1 适配器（所有现有容器插件走这条路径）
	return NewPluginV1Adapter(image)
}

// ── 接口对比总结 ────────────────────────────────────────────────────────────

// 特性                   | v1 (Settings / go-plugin)       | v2 (PluginV2 interface)
// ───────────────────────┼─────────────────────────────────┼─────────────────────────────
// 输入传递               | PLUGIN_ 环境变量               | PluginInput 结构体
// 输出捕获               | stdout 文本解析                | PluginOutput 结构体
// 类型安全               | 无（都是 string）              | 有（JSON Schema + struct）
// 生命周期               | 仅 Execute                      | Validate→Setup→Execute→Teardown
// 元数据/文档            | 无                              | PluginMetadata 自描述
// 错误处理               | exit code                      | error + ExitCode + Stdout
// 向后兼容               | N/A                            | PluginV1Adapter 适配器
// go-plugin (addon)      | Forge / log.Service            | 可统一到 PluginV2 接口

// =============================================================================
// Feature 3: Weblate i18n — 希伯来语（he）动态加载
// =============================================================================
//
// 以下为 Vite i18n 配置及懒加载代码的伪代码/说明。
// 实际修改涉及三个文件：
//   1. web/src/assets/locales/he.json     — 新增翻译文件
//   2. web/vite.config.ts                 — 无需修改（自动发现 locales 目录）
//   3. web/src/compositions/useI18n.ts    — 无需修改（已有懒加载逻辑）
//
// Woodpecker 现有的 i18n 架构已经完美支持动态懒加载：
//   - vite.config.ts 中通过 readdirSync('src/assets/locales/') 自动发现所有 locale 文件
//   - 生成 virtual:vue-i18n-supported-locales 虚拟模块
//   - useI18n.ts 中 loadLocaleMessages() 使用动态 import() 实现懒加载
//   - setI18nLanguage() 在切换语言时按需加载对应 locale JSON
//
// 因此，新增希伯来语只需：(1) 创建 he.json 翻译文件，(2) 重启 dev server 即可。
//
// 以下注释展示了 he.json 文件结构及懒加载代码（已在现有代码中）。

// ===== web/src/assets/locales/he.json (新增文件) =====
//
// {
//   "cancel": "ביטול",
//   "login_to_woodpecker_with": "התחבר ל-Woodpecker עם",
//   "login": "התחברות",
//   "repos": "מאגרים",
//   "repositories": {
//     "title": "מאגרים",
//     "all": {
//       "title": "כל המאגרים",
//       "desc": "מאגרים ממוינים לפי זמן יצירת הצינור האחרון"
//     },
//     "last": {
//       "title": "ביקור אחרון",
//       "desc": "המאגרים שביקרת בהם לאחרונה, ממוינים לפי זמן גישה"
//     }
//   },
//   "docs": "תיעוד",
//   "api": "API",
//   "logout": "התנתקות",
//   "search": "חיפוש…",
//   "username": "שם משתמש",
//   "password": "סיסמה",
//   "back": "חזרה",
//   "unknown_error": "אירעה שגיאה לא ידועה",
//   "documentation_for": "תיעוד עבור \"{topic}\"",
//   "pipeline_feed": "עדכוני צינורות",
//   "empty_list": "לא נמצאו {entity}!",
//   "not_found": {
//     "not_found": "אופס 404, או שאנחנו שברנו משהו או שהייתה לך טעות הקלדה :-/",
//     "back_home": "חזרה לדף הבית"
//   },
//   "errors": {
//     "not_found": "השרת לא הצליח למצוא את האובייקט המבוקש"
//   },
//   "time": {
//     "not_started": "עדיין לא התחיל",
//     "just_now": "הרגע"
//   },
//   "repo": {
//     "manual_pipeline": {
//       "title": "הפעלת צינור ידנית",
//       "trigger": "הפעל צינור",
//       "select_branch": "בחר ענף",
//       "variables": {
//         "delete": "מחק משתנה",
//         "title": "משתני צינור נוספים",
//         "desc": "ציין משתנים נוספים לשימוש בצינור. משתנים בעלי שם זהה יידרסו.",
//         "name": "שם המשתנה",
//         "value": "ערך המשתנה"
//       },
//       "show_pipelines": "הצג צינורות",
//       "no_manual_workflows": "לא נמצאו תהליכי עבודה תואמים. ודא שלפחות תהליך עבודה אחד פועל באירוע ידני."
//     },
//     "pipeline": {
//       "config": "תצורה",
//       "pipeline": "צינור #{pipelineId}"
//     },
//     "settings": {
//       "general": {
//         "general": "כללי",
//         "pipeline_path": {
//           "desc": "הנתיב לקבצי תצורת הצינור בתוך המאגר",
//           "desc_path_example": "\".woodpecker/*.yml\""
//         }
//       }
//     }
//   },
//   "settings": "הגדרות",
//   "info": "מידע",
//   "running_version": "גרסה רצה: {version}",
//   "update_woodpecker": "גרסה {version} זמינה, אנא עדכן את Woodpecker",
//   "admin": {
//     "settings": {
//       "settings": "הגדרות",
//       "orgs": {
//         "orgs": "ארגונים",
//         "desc": "נהל ארגונים במערכת",
//         "view": "צפה",
//         "org_settings": "הגדרות ארגון",
//         "delete_org": "מחק ארגון",
//         "none": "אין ארגונים"
//       },
//       "agents": {
//         "agents": "סוכנים"
//       }
//     }
//   },
//   "org": {
//     "settings": {
//       "not_allowed": "אין לך הרשאה לנהל הגדרות ארגון",
//       "agents": {
//         "desc": "נהל סוכנים עבור ארגון זה"
//       }
//     }
//   },
//   "secrets": {
//     "secrets": "סודות"
//   },
//   "registries": {
//     "registries": "מאגרי תמונות"
//   },
//   "user": {
//     "settings": {
//       "settings": "הגדרות",
//       "general": {
//         "general": "כללי"
//       }
//     }
//   }
// }

// ===== web/vite.config.ts — 无需修改 =====
//
// 现有配置已通过 readdirSync 自动扫描 src/assets/locales/ 目录：
//
//   const filenames = readdirSync('src/assets/locales/')
//     .map((filename) => filename.replace('.json', ''));
//   // filenames 将自动包含 'he'（只要 he.json 存在）
//
//   export const SUPPORTED_LOCALES = ["en", "de", "fr", ..., "he"];
//
// @intlify/unplugin-vue-i18n/vite 插件也已配置：
//
//   VueI18nPlugin({
//     include: path.resolve(__dirname, 'src/assets/locales/**'),
//   })

// ===== web/src/compositions/useI18n.ts — 懒加载逻辑（现有代码，无需修改）=====
//
//   import { SUPPORTED_LOCALES } from 'virtual:vue-i18n-supported-locales';
//
//   const loadLocaleMessages = async (locale: string) => {
//     // 动态 import() 仅在实际切换语言时加载 JSON — 这就是懒加载
//     const messages = (await import(`~/assets/locales/${locale}.json`)).default;
//     i18n.global.setLocaleMessage(locale, messages);
//     return nextTick();
//   };
//
//   export const setI18nLanguage = async (lang: string): Promise<void> => {
//     if (!i18n.global.availableLocales.includes(lang)) {
//       await loadLocaleMessages(lang);  // 懒加载 he.json
//     }
//     i18n.global.locale.value = lang;
//     await setDateLocale(lang);
//   };
//
//   // 注意：希伯来语是 RTL 语言，需要在 App.vue 中增加 RTL 支持：
//   // watch(locale, () => {
//   //   document.documentElement.setAttribute('lang', locale.value);
//   //   document.documentElement.setAttribute(
//   //     'dir',
//   //     ['he', 'ar'].includes(locale.value) ? 'rtl' : 'ltr'
//   //   );
//   // });

// ===== 添加希伯来语的完整步骤总结 =====
//
// 1. 创建 web/src/assets/locales/he.json（使用上述 JSON 内容）
//
// 2. 重启 Vite dev server — readdirSync 自动发现 he.json，虚拟模块自动更新
//
// 3. （可选）RTL 支持：在 App.vue 的 locale watcher 中增加 dir 属性设置
//
// 4. （可选）数字/日期本地化：
//    useDate.ts 中 setDateLocale 传入 'he' 时，Date.toLocaleDateString 等
//    API 会自动使用希伯来语格式。Intl.DisplayNames 也已支持 Hebrew locales。
//
// 5. 验证：
//    - 打开 Woodpecker UI → User Settings → General → Language
//    - 下拉框中应出现 "עברית"（Hebrew）
//    - 切换后页面即时切换为希伯来语，无需整页刷新
//
// 无需修改 vite.config.ts、useI18n.ts 或任何其他现有代码。
// Woodpecker 的 i18n 架构设计天生支持零配置扩展新语言。
