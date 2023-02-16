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
	"time"

	"github.com/dustin/go-humanize"
	"github.com/elastic/apm-server/internal/elasticsearch"
	"github.com/elastic/apm-server/internal/multitenant/util"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/go-docappender"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type DocappenderESConfig struct {
	*elasticsearch.Config `config:",inline"`
	FlushBytes            string        `config:"flush_bytes"`
	FlushInterval         time.Duration `config:"flush_interval"`
	MaxRequests           int           `config:"max_requests"`
	Scaling               struct {
		Enabled *bool `config:"enabled"`
	} `config:"autoscaling"`
}

// TODO: possible to refactor by utilizing base processor?
type MTAppender struct {
	cfgResolver    mtCfgResolver
	clientResolver mtESClientResolver
	memLimit       float64

	logger *logp.Logger

	mu sync.RWMutex
	a  map[string]*docappender.Appender
}

func NewDocappender(
	cfgResolver mtCfgResolver,
	clientResolver mtESClientResolver,
	memLimit float64,
	logger *logp.Logger,
) *MTAppender {
	return &MTAppender{
		cfgResolver:    cfgResolver,
		clientResolver: clientResolver,
		memLimit:       memLimit,
		a:              make(map[string]*docappender.Appender),
		logger:         logger,
	}
}

func (mt *MTAppender) Close(ctx context.Context) error {
	for _, v := range mt.a {
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "failed to close all appenders")
		default:
			v.Close(ctx)
		}
	}
	return nil
}

func (mt *MTAppender) Stats() docappender.Stats {
	// TODO: Implement aggregated stats
	return docappender.Stats{}
}

func (mt *MTAppender) GetWithContext(ctx context.Context) (*docappender.Appender, error) {
	projectID, err := util.GetProjectIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return mt.Get(projectID)
}

func (mt *MTAppender) Get(projectID string) (*docappender.Appender, error) {
	mt.mu.RLock()
	app, ok := mt.a[projectID]
	mt.mu.RUnlock()
	if ok {
		return app, nil
	}

	mt.mu.Lock()
	defer mt.mu.Unlock()
	app, ok = mt.a[projectID]
	if ok {
		return app, nil
	}
	cfg, err := mt.cfgResolver(projectID)
	if err != nil {
		return nil, err
	}
	var flushBytes int
	if cfg.FlushBytes != "" {
		b, err := humanize.ParseBytes(cfg.FlushBytes)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse flush_bytes")
		}
		flushBytes = int(b)
	}
	client, err := mt.clientResolver(cfg.Config)
	if err != nil {
		return nil, err
	}
	var scalingCfg docappender.ScalingConfig
	if enabled := cfg.Scaling.Enabled; enabled != nil {
		scalingCfg.Disabled = !*enabled
	}
	opts := docappender.Config{
		CompressionLevel: cfg.CompressionLevel,
		FlushBytes:       flushBytes,
		FlushInterval:    cfg.FlushInterval,
		MaxRequests:      cfg.MaxRequests,
		Scaling:          scalingCfg,
		Logger:           zap.New(mt.logger.Core(), zap.WithCaller(true)),
	}
	opts = docappenderConfig(opts, mt.memLimit)
	return docappender.New(client, opts)
}

func docappenderConfig(opts docappender.Config, memLimit float64) docappender.Config {
	const logMessage = "%s set to %d based on %0.1fgb of memory"
	// Use 80% of the total memory limit to calculate buffer size
	opts.DocumentBufferSize = int(1024 * memLimit * 0.8)
	if opts.DocumentBufferSize >= 61440 {
		opts.DocumentBufferSize = 61440
	}
	if opts.MaxRequests > 0 {
		return opts
	}
	// This formula yields the following max requests for APM Server sized:
	// 1	2 	4	8	15	30
	// 10	12	14	19	28	46
	maxRequests := int(float64(10) + memLimit*1.5)
	if maxRequests > 60 {
		maxRequests = 60
	}
	opts.MaxRequests = maxRequests
	return opts
}
