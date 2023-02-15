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
	"errors"
	"fmt"
	"sync"

	"github.com/elastic/apm-data/model"
	"github.com/elastic/apm-server/internal/multitenant/util"
)

type batchType interface {
	ProcessBatch(context.Context, *model.Batch) error
}

// baseProcessor represents a wrapper for multitenant batch processors
// of all types. Reference to each batch processor is saved in an in-memory
// map against a project-ID (our tenant identifier).
//
// Processors are created dynamically on discovering a new project-ID.
// The architecture assumes routing of requests for each project based on
// a consistent-hash, ensuring that most of the request for a particular
// project is handled by a specific instance of APM-Server.
//
// Load should be handled via external operators depending on the number
// of projects, traffic volume and the traffic pattern. On scale-up/scale-down
// the distribution of projects across running APM-Servers may change. In
// such cases, the tenant resources for the previous tenants should be
// recycled or disposed.
//
// TODO: Each processor allocates memory on creation. To keep the memory
// requirements under control harvest the unused processors as they age idly.
type baseProcessor[V batchType] struct {
	creator func(string) (V, error)

	mu sync.RWMutex
	a  map[string]V

	// newlyCreated channel allows the caller to consume the new instances created
	// by the processor before ProcessBatch is called. The new instances will
	// be created when a new tenant/project-ID is discovered.
	newlyCreated chan V

	// Useful for aggregators who want to signal a stop for the processor when
	// certain conditions are met.
	stopping chan struct{}
}

func newBaseProcessor[V batchType](
	creator func(string) (V, error),
	newlyCreated chan V,
	stopping chan struct{},
) *baseProcessor[V] {
	return &baseProcessor[V]{
		creator:      creator,
		newlyCreated: newlyCreated,
		stopping:     stopping,

		a: make(map[string]V),
	}
}

// ProcessBatch implements a wrapper over the actual processors. The wrapper
// is responsible for managing the processor instances for specific project-ID.
//
// The project-ID is extracted from the passed context and is then used to
// select or create a new processor instance that will be responsible for
// processing the batch.
func (bp *baseProcessor[V]) ProcessBatch(ctx context.Context, b *model.Batch) error {
	select {
	case <-bp.stopping:
		return errors.New("processor is stopping")
	default:
	}

	projectID, err := util.GetProjectIDFromContext(ctx)
	if err != nil {
		return err
	}

	bp.mu.RLock()
	resource, ok := bp.a[projectID]
	bp.mu.RUnlock()
	if ok {
		return resource.ProcessBatch(ctx, b)
	}

	bp.mu.Lock()
	resource, ok = bp.a[projectID]
	if ok {
		bp.mu.Unlock()
		return resource.ProcessBatch(ctx, b)
	}
	resource, err = bp.creator(projectID)
	if err != nil {
		bp.mu.Unlock()
		return err
	}
	bp.a[projectID] = resource
	bp.mu.Unlock()

	if bp.newlyCreated != nil {
		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to start new aggregator for project ID: %s", projectID)
		case bp.newlyCreated <- resource:
		}
	}
	return resource.ProcessBatch(ctx, b)
}
