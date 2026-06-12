import "context"

// ========== 1. 接口新旧对比 ==========

// PluginV1 旧版插件接口（同步、无上下文控制）
type PluginV1 interface {
    Exec() error
}

// PluginV2 新版插件接口（支持异步上下文、结构化返回）
type PluginV2 interface {
    Execute(ctx context.Context) (*PluginResult, error)
}

type PluginResult struct {
    Status  string
    Outputs map[string]string
}

// ========== 2. 适配器实现 ==========

// PluginV1Adapter 将 V1 插件无缝适配到 V2 架构
type PluginV1Adapter struct {
    legacyPlugin PluginV1
}

func NewPluginAdapter(v1 PluginV1) PluginV2 {
    return &PluginV1Adapter{
        legacyPlugin: v1,
    }
}

// Execute 满足 V2 接口，内部执行 V1 逻辑
func (a *PluginV1Adapter) Execute(ctx context.Context) (*PluginResult, error) {
    errCh := make(chan error, 1)

    // 使用 goroutine 执行旧版阻塞代码
    go func() {
        errCh <- a.legacyPlugin.Exec()
    }()

    select {
    case <-ctx.Done():
        // 支持 V2 的超时与取消特性
        return &PluginResult{Status: "cancelled"}, ctx.Err()
    case err := <-errCh:
        if err != nil {
            return &PluginResult{Status: "failure"}, err
        }
        // V1 无 Outputs 输出，返回默认成功状态
        return &PluginResult{Status: "success", Outputs: map[string]string{}}, nil
    }
}
