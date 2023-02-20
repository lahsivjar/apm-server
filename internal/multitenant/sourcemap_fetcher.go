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

package multitenant

import (
	"context"

	"github.com/elastic/apm-server/internal/multitenant/util"
	"github.com/elastic/apm-server/internal/sourcemap"
	gosourcemap "github.com/go-sourcemap/sourcemap"
)

type SourcemapFetcher struct {
	provider *provider[sourcemap.Fetcher]

	cfgResolver    mtCfgResolver
	clientResolver mtESClientResolver
}

func NewSourcemapFetcher(
	creator func(string) (sourcemap.Fetcher, error),
) *SourcemapFetcher {
	return &SourcemapFetcher{
		provider: newProvider(creator),
	}
}

func (sf *SourcemapFetcher) Fetch(ctx context.Context, name, version, bundleFilePath string) (*gosourcemap.Consumer, error) {
	projectID, err := util.GetProjectIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	res, _, err := sf.provider.get(projectID)
	if err != nil {
		return nil, err
	}
	return res.Fetch(ctx, name, version, bundleFilePath)
}
