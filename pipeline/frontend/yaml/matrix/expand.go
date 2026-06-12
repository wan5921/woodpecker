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

package matrix

import (
	"fmt"
	"sort"
	"strings"

	"go.woodpecker-ci.org/woodpecker/v3/pipeline/frontend/yaml/types"
)

// Expand takes a workflow with matrix configuration and returns expanded workflows.
// Example input:
//
//	matrix:
//	  go: [1.21, 1.22, 1.23]
//	  os: [linux, windows]
//
// Returns 6 workflows (3 go versions × 2 OSes).
func Expand(workflow *types.Workflow) ([]*types.MatrixWorkflow, error) {
	if !workflow.Matrix.HasMatrix() {
		return nil, nil
	}

	keys := make([]string, 0, len(workflow.Matrix))
	for k := range workflow.Matrix {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	matrixValues := make([][]string, 0, len(keys))
	for _, k := range keys {
		matrixValues = append(matrixValues, workflow.Matrix[k])
	}

	combinations := cartesianProduct(matrixValues)

	result := make([]*types.MatrixWorkflow, 0, len(combinations))
	for _, combo := range combinations {
		vars := make(map[string]string)
		nameParts := make([]string, 0, len(keys))

		for i, key := range keys {
			vars[key] = combo[i]
			nameParts = append(nameParts, fmt.Sprintf("%s-%s", key, combo[i]))
		}

		clonedWorkflow := cloneWorkflow(workflow)
		clonedWorkflow.Matrix = nil

		injectMatrixVars(clonedWorkflow, vars)

		result = append(result, &types.MatrixWorkflow{
			Name:     strings.Join(nameParts, "-"),
			Vars:     vars,
			Workflow: clonedWorkflow,
		})
	}

	return result, nil
}

func cartesianProduct(sets [][]string) [][]string {
	if len(sets) == 0 {
		return [][]string{{}}
	}

	result := [][]string{{}}
	for _, set := range sets {
		newResult := make([][]string, 0, len(result)*len(set))
		for _, item := range set {
			for _, combo := range result {
				newCombo := make([]string, len(combo)+1)
				copy(newCombo, combo)
				newCombo[len(combo)] = item
				newResult = append(newResult, newCombo)
			}
		}
		result = newResult
	}

	return result
}

func cloneWorkflow(original *types.Workflow) *types.Workflow {
	cloned := &types.Workflow{
		When:      original.When,
		Workspace: original.Workspace,
		SkipClone: original.SkipClone,
		Labels:    make(map[string]string),
	}

	for k, v := range original.Labels {
		cloned.Labels[k] = v
	}

	cloned.Clone = make(types.ContainerList, len(original.Clone))
	for i, c := range original.Clone {
		cloned.Clone[i] = cloneContainer(c)
	}

	cloned.Steps = make(types.ContainerList, len(original.Steps))
	for i, c := range original.Steps {
		cloned.Steps[i] = cloneContainer(c)
	}

	cloned.Services = make(types.ContainerList, len(original.Services))
	for i, c := range original.Services {
		cloned.Services[i] = cloneContainer(c)
	}

	return cloned
}

func cloneContainer(original types.Container) types.Container {
	cloned := types.Container{
		Name:        original.Name,
		Image:       original.Image,
		Pull:        original.Pull,
		Directory:   original.Directory,
		Failure:     original.Failure,
		Detached:    original.Detached,
		Privileged:  original.Privileged,
		NetworkMode: original.NetworkMode,
	}

	cloned.Commands = make([]string, len(original.Commands))
	copy(cloned.Commands, original.Commands)

	cloned.Entrypoint = make([]string, len(original.Entrypoint))
	copy(cloned.Entrypoint, original.Entrypoint)

	cloned.Ports = make([]string, len(original.Ports))
	copy(cloned.Ports, original.Ports)

	cloned.DNS = make([]string, len(original.DNS))
	copy(cloned.DNS, original.DNS)

	cloned.DNSSearch = make([]string, len(original.DNSSearch))
	copy(cloned.DNSSearch, original.DNSSearch)

	cloned.Devices = make([]string, len(original.Devices))
	copy(cloned.Devices, original.Devices)

	cloned.ExtraHosts = make([]string, len(original.ExtraHosts))
	copy(cloned.ExtraHosts, original.ExtraHosts)

	cloned.Tmpfs = make([]string, len(original.Tmpfs))
	copy(cloned.Tmpfs, original.Tmpfs)

	cloned.Settings = make(map[string]any)
	for k, v := range original.Settings {
		cloned.Settings[k] = v
	}

	cloned.Environment = make(map[string]any)
	for k, v := range original.Environment {
		cloned.Environment[k] = v
	}

	cloned.BackendOptions = make(map[string]any)
	for k, v := range original.BackendOptions {
		cloned.BackendOptions[k] = v
	}

	cloned.Volumes = make(types.Volumes, len(original.Volumes))
	copy(cloned.Volumes, original.Volumes)

	return cloned
}

func injectMatrixVars(workflow *types.Workflow, vars map[string]string) {
	for i := range workflow.Clone {
		injectContainerEnv(&workflow.Clone[i], vars)
	}
	for i := range workflow.Steps {
		injectContainerEnv(&workflow.Steps[i], vars)
	}
	for i := range workflow.Services {
		injectContainerEnv(&workflow.Services[i], vars)
	}
}

func injectContainerEnv(container *types.Container, vars map[string]string) {
	if container.Environment == nil {
		container.Environment = make(map[string]any)
	}
	for k, v := range vars {
		envKey := fmt.Sprintf("MATRIX_%s", strings.ToUpper(k))
		container.Environment[envKey] = v
	}
}
