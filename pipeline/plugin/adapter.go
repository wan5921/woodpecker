// Copyright 2026 Woodpecker Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plugin

import (
	"context"
)

// ============================================================================
// V1 Plugin Interface (Legacy)
// ============================================================================

// PluginV1 defines the legacy plugin interface.
// Characteristics:
// - Uses context.Background() implicitly
// - Returns raw error without structured metadata
// - Settings passed as map[string]interface{}
// - No lifecycle hooks (only Execute)
type PluginV1 interface {
	// Name returns the plugin name.
	Name() string

	// Execute runs the plugin with the given settings.
	Execute(settings map[string]interface{}) error
}

// ============================================================================
// V2 Plugin Interface (New)
// ============================================================================

// PluginV2 defines the new plugin interface.
// Characteristics:
// - Supports context for cancellation and timeout
// - Returns structured PluginResult with metadata
// - Settings passed as typed PluginConfig
// - Supports lifecycle hooks: Initialize, Execute, Cleanup
type PluginV2 interface {
	// Name returns the plugin name.
	Name() string

	// Version returns the plugin version.
	Version() string

	// Initialize prepares the plugin before execution.
	Initialize(ctx context.Context, config PluginConfig) error

	// Execute runs the plugin with context support.
	Execute(ctx context.Context) (*PluginResult, error)

	// Cleanup releases resources after execution.
	Cleanup(ctx context.Context) error
}

// PluginConfig contains the configuration for a V2 plugin.
type PluginConfig struct {
	Settings map[string]interface{}
	Workspace string
	Timeout   int
	Retries   int
}

// PluginResult contains the result of a V2 plugin execution.
type PluginResult struct {
	ExitCode int
	Output   string
	Metadata map[string]interface{}
}

// ============================================================================
// Adapter: V1 to V2
// ============================================================================

// V1ToV2Adapter wraps a V1 plugin to make it compatible with the V2 interface.
// This allows legacy plugins to run in the new plugin system without modification.
type V1ToV2Adapter struct {
	v1Plugin PluginV1
	config   PluginConfig
	result   *PluginResult
}

// NewV1ToV2Adapter creates a new adapter wrapping a V1 plugin.
func NewV1ToV2Adapter(plugin PluginV1) *V1ToV2Adapter {
	return &V1ToV2Adapter{
		v1Plugin: plugin,
	}
}

func (a *V1ToV2Adapter) Name() string {
	return a.v1Plugin.Name()
}

func (a *V1ToV2Adapter) Version() string {
	return "v1-legacy"
}

func (a *V1ToV2Adapter) Initialize(ctx context.Context, config PluginConfig) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	a.config = config
	return nil
}

func (a *V1ToV2Adapter) Execute(ctx context.Context) (*PluginResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	err := a.v1Plugin.Execute(a.config.Settings)

	a.result = &PluginResult{
		ExitCode: 0,
		Output:   "",
		Metadata: map[string]interface{}{
			"adapter": "v1-to-v2",
			"legacy":  true,
		},
	}

	if err != nil {
		a.result.ExitCode = 1
		a.result.Output = err.Error()
	}

	return a.result, err
}

func (a *V1ToV2Adapter) Cleanup(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	a.config = PluginConfig{}
	a.result = nil
	return nil
}

// ============================================================================
// Adapter: V2 to V1 (for backward compatibility)
// ============================================================================

// V2ToV1Adapter wraps a V2 plugin to make it compatible with the V1 interface.
// This allows new plugins to run in legacy systems.
type V2ToV1Adapter struct {
	v2Plugin PluginV2
}

// NewV2ToV1Adapter creates a new adapter wrapping a V2 plugin.
func NewV2ToV1Adapter(plugin PluginV2) *V2ToV1Adapter {
	return &V2ToV1Adapter{
		v2Plugin: plugin,
	}
}

func (a *V2ToV1Adapter) Name() string {
	return a.v2Plugin.Name()
}

func (a *V2ToV1Adapter) Execute(settings map[string]interface{}) error {
	ctx := context.Background()

	config := PluginConfig{
		Settings: settings,
	}

	if err := a.v2Plugin.Initialize(ctx, config); err != nil {
		return err
	}

	defer func() {
		_ = a.v2Plugin.Cleanup(ctx)
	}()

	_, err := a.v2Plugin.Execute(ctx)
	return err
}

// ============================================================================
// Plugin Registry with Version Detection
// ============================================================================

// PluginRegistry manages both V1 and V2 plugins.
type PluginRegistry struct {
	plugins map[string]PluginV2
}

// NewPluginRegistry creates a new plugin registry.
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		plugins: make(map[string]PluginV2),
	}
}

// Register adds a plugin to the registry, automatically adapting V1 plugins.
func (r *PluginRegistry) Register(plugin any) error {
	var v2Plugin PluginV2

	switch p := plugin.(type) {
	case PluginV2:
		v2Plugin = p
	case PluginV1:
		v2Plugin = NewV1ToV2Adapter(p)
	default:
		return ErrUnknownPluginType
	}

	r.plugins[v2Plugin.Name()] = v2Plugin
	return nil
}

// Get retrieves a plugin by name.
func (r *PluginRegistry) Get(name string) (PluginV2, bool) {
	plugin, ok := r.plugins[name]
	return plugin, ok
}

// List returns all registered plugin names.
func (r *PluginRegistry) List() []string {
	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}
