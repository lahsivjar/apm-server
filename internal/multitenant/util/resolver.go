// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package util

import (
	"context"
	"errors"
)

// projectIDKey represents the key for the project ID set in the context.
type projectIDKey struct{}

// SetProjectIDInContext sets the provided project ID in the context
// against a key and returns the derived context if project ID is not empty.
func SetProjectIDInContext(ctx context.Context, projectID string) context.Context {
	if projectID != "" {
		return context.WithValue(ctx, projectIDKey{}, projectID)
	}
	return ctx
}

// GetProjectIDFromContext returns the project ID if it is set in the
// provided context and error otherwise.
func GetProjectIDFromContext(ctx context.Context) (string, error) {
	projectID, ok := ctx.Value(projectIDKey{}).(string)
	if !ok {
		return "", errors.New("failed to get project ID from context")
	}
	return projectID, nil
}
