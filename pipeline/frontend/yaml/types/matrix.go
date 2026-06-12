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

package types

type (
	// Matrix defines a matrix configuration for generating parallel workflows.
	// Example YAML:
	//   matrix:
	//     go: [1.21, 1.22, 1.23]
	//     os: [linux, windows]
	Matrix map[string][]string

	// MatrixWorkflow represents a single expanded workflow from matrix expansion.
	MatrixWorkflow struct {
		// Name is the generated workflow name, e.g. "go-1.21-os-linux"
		Name string
		// Vars contains the matrix variable values for this workflow
		Vars map[string]string
		// Workflow is the actual workflow configuration
		Workflow *Workflow
	}
)

// HasMatrix returns true if the matrix has at least one dimension.
func (m Matrix) HasMatrix() bool {
	return len(m) > 0
}

// Dimensions returns the number of dimensions in the matrix.
func (m Matrix) Dimensions() int {
	return len(m)
}

// TotalCombinations calculates the total number of workflow combinations.
func (m Matrix) TotalCombinations() int {
	if len(m) == 0 {
		return 0
	}
	total := 1
	for _, values := range m {
		total *= len(values)
	}
	return total
}
