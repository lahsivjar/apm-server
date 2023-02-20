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
	"sync"
)

type resource interface{}

// provider represents a multitenant resource. The resource can be anything
// that is required to behave in a multitenant way. Reference to each resource
// will be saved in an in-memory map against the project-ID (our tenant identifier).
//
// Resources will be created dynamically on discovery of a new project-ID. The
// architecture assumes routing of requests for each project based on a
// consistent-hash, ensuring that most of the request for a particular project is
// handled by a specific instance of APM-Server.
//
// Load should be handled via external operators depending on the number of projects,
// traffic volume and the traffic pattern. On scale-up/scale-down the distribution of
// projects across running APM-Servers may change. In such cases, the tenant resources
// for the previous tenants should be recycled or disposed.
//
// TODO: Instead of creating a provider instance per resource, we can create a default
// provider which saves all the resources in a single map.
// TODO: Implement harvesting of unused idle tenants after a timeout. This will allow
// us to get a list of project-IDs that a particular APM-Server instance is serving and
// can be used to run tenant specific long-running workloads.
type provider[V resource] struct {
	creator func(string) (V, error)

	mu sync.RWMutex
	m  map[string]V
}

func newProvider[V resource](creator func(string) (V, error)) *provider[V] {
	return &provider[V]{
		creator: creator,
		m:       make(map[string]V),
	}
}

// get returns a cached resource for a project-ID. If the cached resource is not available,
// then it will create the resource and put it into the cache for future lookups.
func (p *provider[V]) get(projectID string) (V, bool, error) {
	p.mu.RLock()
	res, ok := p.m[projectID]
	p.mu.RUnlock()
	if ok {
		return res, false, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	res, ok = p.m[projectID]
	if ok {
		return res, false, nil
	}
	var err error
	res, err = p.creator(projectID)
	if err != nil {
		return res, false, err
	}
	p.m[projectID] = res
	return res, true, nil
}

// forEach runs the provided function against all cached resource entries.
func (p *provider[V]) forEach(do func(projectID string, val V)) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for k, v := range p.m {
		do(k, v)
	}
}
