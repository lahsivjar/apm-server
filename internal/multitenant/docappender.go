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
	"io"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/elastic/apm-data/model"
	"github.com/elastic/apm-server/internal/elasticsearch"
	"github.com/elastic/apm-server/internal/multitenant/util"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/go-docappender"
	"github.com/pkg/errors"
	"go.elastic.co/fastjson"
	"go.uber.org/zap"
)

// DocappenderESConfig refers to the elasticsearch configuration requrired for docappender.
type DocappenderESConfig struct {
	*elasticsearch.Config `config:",inline"`
	FlushBytes            string        `config:"flush_bytes"`
	FlushInterval         time.Duration `config:"flush_interval"`
	MaxRequests           int           `config:"max_requests"`
	Scaling               struct {
		Enabled *bool `config:"enabled"`
	} `config:"autoscaling"`
}

// Docappender refers to multitenant wrapper over the actual docappender.
type Docappender struct {
	provider *provider[*docappender.Appender]
	pool     *sync.Pool
	logger   *logp.Logger

	mu sync.RWMutex
	a  map[string]*docappender.Appender
}

// NewDocappender returns a multitenant docappender wrapper.
func NewDocappender(
	cfgResolver mtCfgResolver,
	clientResolver mtESClientResolver,
	memLimit float64,
	logger *logp.Logger,
) *Docappender {
	var pool sync.Pool
	pool.New = func() any {
		return &pooledReader{pool: &pool}
	}
	return &Docappender{
		provider: newProvider(func(projectID string) (*docappender.Appender, error) {
			cfg, err := cfgResolver(projectID)
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
			client, err := clientResolver(cfg.Config)
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
				Logger:           zap.New(logger.Core(), zap.WithCaller(true)),
			}
			opts = docappenderConfig(opts, memLimit)
			return docappender.New(client, opts)
		}),
		pool:   &pool,
		a:      make(map[string]*docappender.Appender),
		logger: logger,
	}
}

// Close closes all docappenders for all the project-IDs.
func (a *Docappender) Close(ctx context.Context) error {
	for _, v := range a.a {
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "failed to close all appenders")
		default:
			v.Close(ctx)
		}
	}
	return nil
}

// Stats returns aggregated stats for all docappenders.
func (a *Docappender) Stats() docappender.Stats {
	// TODO: Implement aggregated stats
	return docappender.Stats{}
}

// ProcessBatch processes the batch by resolving the project-ID and seleting the
// appropriate appender.
func (a *Docappender) ProcessBatch(ctx context.Context, b *model.Batch) error {
	projectID, err := util.GetProjectIDFromContext(ctx)
	if err != nil {
		return err
	}

	agg, _, err := a.provider.get(projectID)
	if err != nil {
		return err
	}

	for _, event := range *b {
		r := a.pool.Get().(*pooledReader)
		if err := event.MarshalFastJSON(&r.jsonw); err != nil {
			r.reset()
			return err
		}
		r.indexBuilder.WriteString(event.DataStream.Type)
		r.indexBuilder.WriteByte('-')
		r.indexBuilder.WriteString(event.DataStream.Dataset)
		r.indexBuilder.WriteByte('-')
		r.indexBuilder.WriteString(event.DataStream.Namespace)
		index := r.indexBuilder.String()
		if err := agg.Add(ctx, index, r); err != nil {
			r.reset()
			return err
		}
	}
	return nil
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

type pooledReader struct {
	pool         *sync.Pool
	jsonw        fastjson.Writer
	indexBuilder strings.Builder
}

func (r *pooledReader) Read(p []byte) (int, error) {
	panic("should've called WriteTo")
}

func (r *pooledReader) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(r.jsonw.Bytes())
	r.reset()
	return int64(n), err
}

func (r *pooledReader) reset() {
	r.jsonw.Reset()
	r.indexBuilder.Reset()
	r.pool.Put(r)
}
