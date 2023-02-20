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
	"github.com/elastic/apm-server/x-pack/apm-server/aggregation/servicesummarymetrics"
	"github.com/elastic/apm-server/x-pack/apm-server/aggregation/servicetxmetrics"
	"github.com/elastic/apm-server/x-pack/apm-server/aggregation/spanmetrics"
	"github.com/elastic/apm-server/x-pack/apm-server/aggregation/txmetrics"
	"github.com/elastic/elastic-agent-libs/monitoring"
	"golang.org/x/sync/errgroup"
)

type aggregatorType interface {
	*txmetrics.Aggregator | *servicetxmetrics.Aggregator | *spanmetrics.Aggregator | *servicesummarymetrics.Aggregator

	ProcessBatch(ctx context.Context, b *model.Batch) error
	Run() error
	Stop(context.Context) error
}

// AggregatorProcessor represents a multitenant processor for aggregations.
// All aggregators wrapped with this processor will behave as a multitenant
// aggregator.
type AggregatorProcessor[V aggregatorType] struct {
	provider     *provider[V]
	stopMu       sync.Mutex
	stopping     chan struct{}
	newlyCreated chan V
}

// NewAggregator wraps an existing aggregator to allow for dynamic creation
// of the aggregators for each tenant/project-ID on discovery of the new
// project-ID.
func NewAggregator[V aggregatorType](
	creator func(string) (V, error),
) *AggregatorProcessor[V] {
	return &AggregatorProcessor[V]{
		provider:     newProvider(creator),
		stopping:     make(chan struct{}),
		newlyCreated: make(chan V),
	}
}

func (rp *AggregatorProcessor[V]) ProcessBatch(ctx context.Context, b *model.Batch) error {
	select {
	case <-rp.stopping:
		return errors.New("processor is stopping")
	default:
	}

	projectID, err := util.GetProjectIDFromContext(ctx)
	if err != nil {
		return err
	}

	res, newlyCreated, err := rp.provider.get(projectID)
	if err != nil {
		return err
	}

	if newlyCreated {
		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to start new aggregator for project ID: %s", projectID)
		case rp.newlyCreated <- res:
		}
	}

	return res.ProcessBatch(ctx, b)
}

// Run runs all the aggregator in its own goroutine. It also handles new aggregators
// created dynamically on identifying request for a new project.
func (rp *AggregatorProcessor[V]) Run() error {
	eg := &errgroup.Group{}
	go func() {
		for agg := range rp.newlyCreated {
			eg.Go(agg.Run)
		}
	}()
	return eg.Wait()
}

// Stop stops all the aggregators for all the project-IDs.
func (rp *AggregatorProcessor[V]) Stop(ctx context.Context) error {
	rp.stopMu.Lock()
	select {
	case <-rp.stopping:
		return nil
	default:
		close(rp.stopping)
		close(rp.newlyCreated)
	}
	rp.stopMu.Unlock()

	eg := &errgroup.Group{}
	rp.provider.forEach(func(projectID string, val V) {
		eg.Go(func() error {
			return val.Stop(ctx)
		})
	})
	return eg.Wait()
}

// CollectMonitoring collects aggregated monitoring for all aggregators.
func (rp *AggregatorProcessor[V]) CollectMonitoring(_ monitoring.Mode, v monitoring.Visitor) {
	// TODO: Collect aggregated monitoring
}
