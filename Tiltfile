custom_build(
  'mt-apm-server',
  'docker build -f ./packaging/docker/Dockerfile -t $EXPECTED_REF .',
  ['go.mod', 'go.sum', 'Makefile', '*.mk', '.git', 'cmd', 'internal', 'x-pack'],
)

# Build and install the APM integration package whenever source under
# "apmpackage" changes.
script_dir = os.path.join(config.main_dir, 'script')
run_with_go_ver = os.path.join(script_dir, 'run_with_go_ver')
local_resource(
  'apmpackage:tenant1',
  cmd = [os.path.join(script_dir, 'run_with_go_ver'), 'go', 'run', './cmd/runapm -init'],
  dir = 'systemtest',
  deps = ['apmpackage'],
  resource_deps=['kibana:kibana:tenant1'],
)

local_resource(
  'apmpackage:tenant2',
  cmd = [os.path.join(script_dir, 'run_with_go_ver'), 'KIBANA_PORT=5602', 'go', 'run', './cmd/runapm -init'],
  dir = 'systemtest',
  deps = ['apmpackage'],
  resource_deps=['kibana:kibana:tenant2'],
)

k8s_yaml(kustomize('testing/infra/k8s/overlays/multitenant'))

k8s_kind('Agent', image_json_path='{.spec.image}')
k8s_kind('Kibana')
k8s_kind('Elasticsearch')

k8s_resource('elastic-operator', objects=['eck-trial-license:Secret:elastic-system'])

# Run APM-Server as multitenant
k8s_resource('mt-apm-server', port_forwards=8200)

# Resources for tenant1
k8s_resource('kibana:kibana:tenant1', port_forwards=5601)
k8s_resource('elasticsearch:elasticsearch:tenant1', port_forwards=9200, objects=['elasticsearch-admin:Secret:tenant1'])

# Resources for tenant2
k8s_resource('kibana:kibana:tenant2', port_forwards=5602)
k8s_resource('elasticsearch:elasticsearch:tenant2', port_forwards=9201, objects=['elasticsearch-admin:Secret:tenant2'])

# Delete ECK entirely on `tilt down`, to ensure `tilt up` starts from a clean slate.
#
# Without this, ECK refuses to recreate the trial license. That's expected
# for actual trial use, but we rely an Enterprise feature for local development:
# installing the integration package by uploading to Fleet.
if config.tilt_subcommand == "down":
  print(local("kubectl delete --ignore-not-found namespace/elastic-system"))

