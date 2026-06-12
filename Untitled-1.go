// CI 配置结构体定义
type PipelineConfig struct {
    Matrix map[string][]string `yaml:"matrix,omitempty"`
    Steps  []Step              `yaml:"steps"`
    // ... 其他全局配置
}

type Step struct {
    Name     string            `yaml:"name"`
    Image    string            `yaml:"image"`
    Commands []string          `yaml:"commands"`
    Env      map[string]string `yaml:"environment,omitempty"`
}

// 矩阵生成伪代码
func GenerateMatrixPipelines(base *PipelineConfig) []*PipelineConfig {
    if len(base.Matrix) == 0 {
        return []*PipelineConfig{base}
    }

    // 1. 计算所有矩阵维度的笛卡尔积（如 go: [1.21, 1.22] 和 os: [linux]）
    combinations := generateCartesianProduct(base.Matrix)

    var pipelines []*PipelineConfig
    // 2. 遍历每一个组合，生成独立的流水线配置
    for _, combo := range combinations {
        // 深拷贝基础配置
        newConfig := cloneConfig(base)

        // 3. 将矩阵变量注入到每个步骤的环境变量中
        for i := range newConfig.Steps {
            if newConfig.Steps[i].Env == nil {
                newConfig.Steps[i].Env = make(map[string]string)
            }
            for key, val := range combo {
                // 注入环境变量，例如 GO_VERSION=1.21
                newConfig.Steps[i].Env[key] = val
            }
        }
        pipelines = append(pipelines, newConfig)
    }

    return pipelines
}
