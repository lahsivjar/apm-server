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
	"sync"

	"github.com/elastic/apm-server/internal/multitenant/util"
	"github.com/elastic/apm-server/internal/sourcemap"
	gosourcemap "github.com/go-sourcemap/sourcemap"
)

type SourcemapFetcher struct {
	creator func(string) (sourcemap.Fetcher, error)

	cfgResolver    mtCfgResolver
	clientResolver mtESClientResolver

	mu sync.RWMutex
	a  map[string]sourcemap.Fetcher
}

func NewSourcemapFetcher(
	creator func(string) (sourcemap.Fetcher, error),
) *SourcemapFetcher {
	return &SourcemapFetcher{
		creator: creator,
		a:       make(map[string]sourcemap.Fetcher),
	}
}

func (sf *SourcemapFetcher) Fetch(ctx context.Context, name, version, bundleFilePath string) (*gosourcemap.Consumer, error) {
	projectID, err := util.GetProjectIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	sf.mu.RLock()
	fetcher, ok := sf.a[projectID]
	sf.mu.RUnlock()
	if ok {
		return fetcher.Fetch(ctx, name, version, bundleFilePath)
	}

	sf.mu.Lock()
	fetcher, ok = sf.a[projectID]
	if ok {
		sf.mu.Unlock()
		return fetcher.Fetch(ctx, name, version, bundleFilePath)
	}
	fetcher, err = sf.creator(projectID)
	if err != nil {
		sf.mu.Unlock()
		return nil, err
	}
	sf.a[projectID] = fetcher
	sf.mu.Unlock()

	return fetcher.Fetch(ctx, name, version, bundleFilePath)
}
